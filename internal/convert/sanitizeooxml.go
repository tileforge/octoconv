package convert

// This file (sanitizeooxml.go): the OOXML sanitize path -- a REBUILD of the
// container, not an edit of it.
//
// WHY REBUILD
//
// SANI-04 requires the removed construct to be physically absent. Editing an
// archive in place leaves the old local file header, or a hole, or a central
// directory record pointing at bytes that are still there; a reader that walks
// local headers instead of the central directory finds what the central
// directory no longer lists. Writing a new archive containing only the parts
// that survive makes absence structural rather than asserted -- the same
// argument the PDF path makes for going through PostScript.
//
// REMOVING A PART IS THREE EDITS, NOT ONE (60-RESEARCH.md Q2)
//
// Dropping word/vbaProject.bin and stopping there produces a file Word
// refuses to open. Three other places name it:
//
//	1. [Content_Types].xml  -- <Override PartName="/word/vbaProject.bin">,
//	   and <Default Extension="bin"> once no .bin part remains.
//	2. word/_rels/document.xml.rels -- <Relationship … Target="vbaProject.bin">.
//	   An orphaned relationship breaks opening exactly as reliably as a
//	   missing content type.
//	3. [Content_Types].xml again -- /word/document.xml's own Override carries
//	   the macroEnabled content type, which must become the plain one (D-06).
//
// That third edit is why D-05 and D-06 are one piece of code rather than two
// tasks: the macro-enabled content type lives in the file we are already
// rewriting. A partial job here is the worst outcome available -- the job
// finishes GREEN and hands the client a document that will not open, which is
// worse than not sanitizing at all, because nothing reports it.
//
// WHY THE OUTPUT EXTENSION CHANGES
//
// .docm becomes .docx, .xlsm becomes .xlsx. A container whose content type
// promises macros that are no longer there is a lie of exactly the kind Phase
// 59 spent a plan removing from comments. The extension follows the content
// type because the content type is what changed.
//
// WHY NO SECOND STRUCTURAL CEILING
//
// This code deliberately does NOT introduce its own zip entry/size bound.
// Two already exist -- internal/api's inspectMaxZipDirEntries (applied at
// upload, before any of this runs) and internal/inspect's maxZipEntriesScanned
// -- and a third declaration would be a third place to forget when the number
// moves. The worker's own ENGINE_TIMEOUT and container memory limit are the
// backstop for anything that reaches here.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

func init() {
	for _, f := range []string{"docm", "docx", "xlsm", "xlsx", "pptm", "pptx", "dotm", "potm", "ppsm"} {
		sanitizeFormats[f] = sanitizeOOXML
	}
}

// macroEnabledContentTypes maps every macro-enabled OOXML content type to its
// macro-free equivalent.
//
// A TABLE, not a switch. The registry, the error codes, the finding codes and
// the engine classes are all declared this way in this project, and the reason
// is the same each time: the next format is a row, and a row is reviewable
// next to its neighbours in a way a new `case` buried in a function is not.
var macroEnabledContentTypes = map[string]string{
	"application/vnd.ms-word.document.macroEnabled.main+xml":               "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
	"application/vnd.ms-word.template.macroEnabledTemplate.main+xml":       "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml",
	"application/vnd.ms-excel.sheet.macroEnabled.main+xml":                 "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml",
	"application/vnd.ms-excel.template.macroEnabled.main+xml":              "application/vnd.openxmlformats-officedocument.spreadsheetml.template.main+xml",
	"application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml":     "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml",
	"application/vnd.ms-powerpoint.template.macroEnabledTemplate.main+xml": "application/vnd.openxmlformats-officedocument.presentationml.template.main+xml",
	"application/vnd.ms-powerpoint.slideshow.macroEnabled.main+xml":        "application/vnd.openxmlformats-officedocument.presentationml.slideshow.main+xml",
}

// macroEnabledExtensions maps each macro-enabled file extension to its
// macro-free equivalent, in the same table form and for the same reason.
var macroEnabledExtensions = map[string]string{
	"docm": "docx",
	"dotm": "dotx",
	"xlsm": "xlsx",
	"xltm": "xltx",
	"pptm": "pptx",
	"potm": "potx",
	"ppsm": "ppsx",
}

// SanitizedOOXMLExtension returns the extension a sanitized copy of a file in
// format should carry -- the macro-free equivalent when one exists, otherwise
// the input's own extension unchanged.
func SanitizedOOXMLExtension(format string) string {
	f := NormalizeFormat(format)
	if plain, ok := macroEnabledExtensions[f]; ok {
		return plain
	}
	return f
}

// ooxmlThreatClasses are the classes the OOXML path removes. Declared per
// format for the same reason pdfThreatClasses is: adding a class to the
// vocabulary must not silently extend what this format claims to remove.
var ooxmlThreatClasses = []ThreatClass{
	ThreatVBAMacro,
	ThreatOLEObject,
	ThreatEmbeddedFile,
	ThreatExternalReference,
	ThreatDocumentMetadata,
}

// ooxmlLostCapabilities are the features removal destroys. Macro
// functionality is the obvious one and the only one a client is likely to
// miss deliberately.
var ooxmlLostCapabilities = []string{
	"vba_macro_functionality",
	"embedded_ole_objects",
	"document_properties",
}

type ctTypes struct {
	XMLName  xml.Name     `xml:"Types"`
	Defaults []ctDefault  `xml:"Default"`
	Override []ctOverride `xml:"Override"`
}

type ctDefault struct {
	Extension   string `xml:"Extension,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type ctOverride struct {
	PartName    string `xml:"PartName,attr"`
	ContentType string `xml:"ContentType,attr"`
}

type relRelationships struct {
	XMLName xml.Name `xml:"Relationships"`
	Rel     []rel    `xml:"Relationship"`
}

type rel struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr,omitempty"`
}

// isDroppedPart decides whether a part is carried into the rebuilt archive.
//
// The OLE check reads the part's CONTENT, not its name. Macro and object
// storage is portable; the part name is not. A checker that drops
// "word/vbaProject.bin" by name and nothing else catches only an opponent who
// left the default name in place -- which is to say, catches nothing that was
// trying.
func isDroppedPart(name string, content []byte) (bool, ThreatClass) {
	if ooxmlMacroParts[name] {
		return true, ThreatVBAMacro
	}
	if IsOLECFBFromPrefix(content) {
		return true, ThreatOLEObject
	}
	if strings.HasPrefix(name, "docProps/") {
		return true, ThreatDocumentMetadata
	}
	// Embedded objects and attachments live under */embeddings/ regardless of
	// the individual part's name.
	if strings.Contains(name, "/embeddings/") {
		return true, ThreatEmbeddedFile
	}
	return false, ""
}

// resolveTarget resolves a relationship Target against the part its .rels file
// describes, so "vbaProject.bin" inside word/_rels/document.xml.rels resolves
// to "word/vbaProject.bin".
//
// Without this, a relationship pointing at a dropped part would look like a
// relationship to some other file entirely and be carried forward, leaving the
// orphan this whole file exists to avoid.
func resolveTarget(relsPath, target string) string {
	target = strings.TrimPrefix(target, "/")
	base := path.Dir(path.Dir(relsPath)) // strip "_rels/<name>.rels"
	if base == "." {
		base = ""
	}
	return path.Clean(path.Join(base, target))
}

func rewriteContentTypes(raw []byte, dropped map[string]bool, anyBinLeft bool) ([]byte, error) {
	var t ctTypes
	if err := xml.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse [Content_Types].xml: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	buf.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)

	for _, d := range t.Defaults {
		// A Default for an extension no surviving part uses is not fatal to
		// opening, but it is a dangling declaration of exactly the kind this
		// file removes elsewhere, and "bin" specifically is the macro one.
		if strings.EqualFold(d.Extension, "bin") && !anyBinLeft {
			continue
		}
		fmt.Fprintf(&buf, `<Default Extension="%s" ContentType="%s"/>`,
			xmlAttr(d.Extension), xmlAttr(d.ContentType))
	}
	for _, o := range t.Override {
		if dropped[strings.TrimPrefix(o.PartName, "/")] {
			continue
		}
		ct := o.ContentType
		if plain, ok := macroEnabledContentTypes[ct]; ok {
			ct = plain
		}
		fmt.Fprintf(&buf, `<Override PartName="%s" ContentType="%s"/>`,
			xmlAttr(o.PartName), xmlAttr(ct))
	}
	buf.WriteString(`</Types>`)
	return buf.Bytes(), nil
}

func rewriteRels(relsPath string, raw []byte, dropped map[string]bool) ([]byte, bool, error) {
	var rs relRelationships
	if err := xml.Unmarshal(raw, &rs); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", relsPath, err)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	buf.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)

	removedExternal := false
	for _, r := range rs.Rel {
		if strings.EqualFold(r.TargetMode, "External") {
			// An external relationship is the OOXML form of "phone home when
			// opened" -- the same client-visible fact as a PDF /URI action.
			removedExternal = true
			continue
		}
		if dropped[resolveTarget(relsPath, r.Target)] {
			continue
		}
		fmt.Fprintf(&buf, `<Relationship Id="%s" Type="%s" Target="%s"/>`,
			xmlAttr(r.ID), xmlAttr(r.Type), xmlAttr(r.Target))
	}
	buf.WriteString(`</Relationships>`)
	return buf.Bytes(), removedExternal, nil
}

// xmlAttr escapes a value for use inside a double-quoted XML attribute.
// encoding/xml's own EscapeText targets character data and escapes newlines
// differently; this is the attribute-context escape.
func xmlAttr(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

// sanitizeOOXML rebuilds inPath into outPath without the macro project, OLE
// objects, embedded attachments, document properties or external references,
// and with the container's own wiring made consistent with what remains.
func sanitizeOOXML(ctx context.Context, inPath, outPath string) (SanitizeResult, error) {
	zr, err := zip.OpenReader(inPath)
	if err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize ooxml: open: %w", err)
	}
	defer zr.Close()

	// Pass 1: read every part and decide its fate. The whole set is needed
	// before any rewriting, because [Content_Types].xml and the .rels files
	// must be rewritten against the FINAL survivor set, not against a partial
	// one -- a relationship to a part dropped later in the walk would survive.
	type part struct {
		name    string
		content []byte
	}
	var parts []part
	dropped := map[string]bool{}
	removedClasses := map[ThreatClass]bool{}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return SanitizeResult{}, fmt.Errorf("sanitize ooxml: open part %s: %w", f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return SanitizeResult{}, fmt.Errorf("sanitize ooxml: read part %s: %w", f.Name, err)
		}
		if drop, class := isDroppedPart(f.Name, content); drop {
			dropped[f.Name] = true
			removedClasses[class] = true
			continue
		}
		parts = append(parts, part{name: f.Name, content: content})
	}

	anyBinLeft := false
	for _, p := range parts {
		if strings.EqualFold(path.Ext(p.name), ".bin") {
			anyBinLeft = true
			break
		}
	}

	// Pass 2: rewrite the wiring, then write a new archive.
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, p := range parts {
		content := p.content
		switch {
		case p.name == "[Content_Types].xml":
			content, err = rewriteContentTypes(p.content, dropped, anyBinLeft)
			if err != nil {
				return SanitizeResult{}, fmt.Errorf("sanitize ooxml: %w", err)
			}
		case strings.HasSuffix(p.name, ".rels"):
			var external bool
			content, external, err = rewriteRels(p.name, p.content, dropped)
			if err != nil {
				return SanitizeResult{}, fmt.Errorf("sanitize ooxml: %w", err)
			}
			if external {
				removedClasses[ThreatExternalReference] = true
			}
		}

		// Fixed timestamp: a sanitized copy carrying the sanitizing server's
		// clock is a small piece of the same metadata this operation exists to
		// remove, and it would make the output non-reproducible besides.
		hdr := &zip.FileHeader{Name: p.name, Method: zip.Deflate}
		hdr.SetModTime(zipEpoch())
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return SanitizeResult{}, fmt.Errorf("sanitize ooxml: create %s: %w", p.name, err)
		}
		if _, err := w.Write(content); err != nil {
			return SanitizeResult{}, fmt.Errorf("sanitize ooxml: write %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize ooxml: finish archive: %w", err)
	}

	if err := os.WriteFile(outPath, out.Bytes(), 0o644); err != nil {
		return SanitizeResult{}, fmt.Errorf("sanitize ooxml: write output: %w", err)
	}

	_ = ctx // the rebuild is pure Go; no external process to cancel

	return SanitizeResult{
		Removed:          append([]ThreatClass(nil), ooxmlThreatClasses...),
		LostCapabilities: append([]string(nil), ooxmlLostCapabilities...),
	}, nil
}

// zipEpoch is the fixed modification time stamped on every rebuilt part.
// Declared as a function rather than a package var so no init-order question
// arises about it.
func zipEpoch() time.Time {
	return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
}
