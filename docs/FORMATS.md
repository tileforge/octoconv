# Formats and operations

[Back to OctoConv](../README.md) · [Documentation](README.md)

Every job carries an **operation** as well as a format pair. `convert` is the default;
`sanitize` rebuilds a file in its own format with active content stripped; `render` draws a
CAD file as a PNG preview and adds its measurements as a second output with role
`measurements` ([`docs/MEASUREMENTS.md`](MEASUREMENTS.md)).

| Class | Converts | Engine |
|---|---|---|
| `image` | `png` `jpg` `webp` `heic` `tiff`, any-to-any | [libvips][libvips] |
| `document` | `docx` `xlsx` `pptx` `odt` `ods` `odp` → `pdf`; `docx`↔`odt`, `xlsx`↔`ods`, `pptx`↔`odp` | LibreOffice headless |
| `html` | `html` → `pdf` | headless Chromium |
| `audio` | `mp3` `wav` `m4a` `ogg` and video containers → `txt` `srt` `vtt` `json` | whisper.cpp |
| `av` | `mov`/`avi`/`mkv`/`webm` → `mp4`; `mp4` → `webm`; audio extract; thumbnail | ffmpeg |
| `ocr` | `jpg` `png` `tiff` `webp` `pdf` → `txt` `hocr` `alto` | Tesseract |
| `pandoc` | `md` `rst` `org` `latex` `html` `docx` `epub`, across a curated allowlist | pandoc |
| `probe` | `sanitize`: `pdf` `docx` `xlsx` `pptx` `jpg` `png` rebuilt in the same format, and `docm` `xlsm` `pptm` rebuilt into their macro-free extension; also serves `POST /v1/inspect` | in-process |
| `tectonic` | `latex` → `pdf`, single file or multi-file project | [tectonic][tectonic] |
| `cad` | `step`/`stp` and `iges`/`igs` → `stl`, binary, millimetres; `dxf` → `svg` `pdf`; `dwg` → `svg` `pdf` `png`; `render`: `step` `iges` `stl` `dxf` `dwg` → `png` | OCCT through cadquery-ocp; VTK for 3D previews; ezdxf and librsvg for DXF and DWG |

`GET /v1/formats` returns this table as the service itself sees it — derived from the
converter registry, never hand-maintained.

[libvips]: https://www.libvips.org/
[tectonic]: https://tectonic-typesetting.github.io/
