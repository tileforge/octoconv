package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/tileforge/octoconv/internal/presets"
)

func TestKeyedSmallBodyMassIsReleasedAtTheShortWindow(t *testing.T) {
	const n = 80
	window := smallBodyWindow(maxPresetBodyBytes)
	answerLimit := window + time.Second
	closeLimit := refusalDrainGrace + time.Second
	settle := max(2*time.Second, smallBodyWindow(smallBodyMaxBytes)+time.Second)
	if answerLimit >= intakeReadDeadline || settle >= intakeReadDeadline {
		t.Fatalf("smallBodyWindow(maxPresetBodyBytes) + 1 s = %v and the settle %v are not shorter than intakeReadDeadline %v: the mass cannot tell a body cut at the short window from one held for the body window", answerLimit, settle, intakeReadDeadline)
	}
	marks := &readMarks{}
	ts := startShippedServer(t, marks.wrap(heapProbeServer().Routes()))
	head := headOnTheLine(strings.TrimSuffix(requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", 4000), "\r\n"))
	burst := preAuthRefusals(t, ts.Listener.Addr().String(), head, n, 0, 0, 1, 0, settle)
	readers := marks.readers.Load()
	statuses := map[int]int{}
	var slowest, latest time.Duration
	for i, o := range burst.outcomes {
		if err := errors.Join(o.writeErr, o.err); err != nil {
			t.Errorf("request %d: %v", i, err)
			continue
		}
		statuses[o.status]++
		answer, closed := o.answered.Sub(o.headSent), o.closed.Sub(o.answered)
		slowest, latest = max(slowest, answer), max(latest, closed)
		if o.status != http.StatusBadRequest && o.status != http.StatusTooManyRequests {
			t.Errorf("request %d: status %d, want 400 or 429", i, o.status)
		}
		if answer > answerLimit {
			t.Errorf("request %d: answered %d %v after its head, want no later than smallBodyWindow(maxPresetBodyBytes) + 1 s = %v: the stalled JSON body held its request", i, o.status, answer, answerLimit)
		}
		if closed > closeLimit {
			t.Errorf("request %d: the server closed the connection %v after the answer, want no later than refusalDrainGrace + 1 s = %v", i, closed, closeLimit)
		}
	}
	if statuses[http.StatusBadRequest] == 0 || int64(statuses[http.StatusBadRequest]) != readers {
		t.Errorf("%d answers are 400 and %d requests read the body, want at least one 400 and every 400 a request that read the body", statuses[http.StatusBadRequest], readers)
	}
	bound := preAuthRequestHeapBytes()
	t.Logf("SMALLBODY | POST /v1/presets | n %d | statuses %v | readers %d | slowest answer %v | latest close %v | tail %d B (%.2f MiB) | bound %d B | window %v", n, statuses, readers, slowest.Round(time.Millisecond), latest.Round(time.Millisecond), burst.tail, mib(burst.tail), bound, window)
	if burst.tail > bound {
		t.Errorf("%d keyed POST /v1/presets with a stalled JSON body still hold %d bytes (%.1f MiB) of heap and stack %v after the last head, above preAuthRequestHeapBytes = %d (%.3f MiB)", n, burst.tail, mib(burst.tail), settle, bound, mib(bound))
	}
}

func probePresetAdmin() *fakePresetAdmin {
	admin := defaultFakePresetAdmin()
	admin.getForClientErr = nil
	admin.getForClientResult = &presets.Preset{Name: "probe", Version: 1, Scope: presets.ScopeUser, TargetFormat: "png", IsActive: true}
	return admin
}

func honestPresetBody() string {
	prefix, suffix := `{"name":"probe","target_format":"png","description":"`, `"}`
	return prefix + strings.Repeat("d", maxPresetBodyBytes-128-len(prefix)-len(suffix)) + suffix
}

func splitBody(body string, n int) []string {
	step := len(body) / n
	out := make([]string, 0, n)
	for i := range n {
		hi := (i + 1) * step
		if i == n-1 {
			hi = len(body)
		}
		out = append(out, body[i*step:hi])
	}
	return out
}

func TestHonestSmallBodyPassesWithinTheShortWindow(t *testing.T) {
	window := smallBodyWindow(maxPresetBodyBytes)
	body := honestPresetBody()
	if len(body) < maxPresetBodyBytes-256 || len(body) > maxPresetBodyBytes {
		t.Fatalf("honest body is %d bytes, want between maxPresetBodyBytes - 256 and maxPresetBodyBytes = %d", len(body), maxPresetBodyBytes)
	}
	head := requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", int64(len(body)))

	t.Run("kept connection", func(t *testing.T) {
		var news atomic.Int64
		srv := heapProbeServer()
		srv.presetAdmin = probePresetAdmin()
		ts := startShippedServer(t, srv.Routes(), countNewConns(&news))
		k := dialKept(t, ts)
		short := `{"name":"probe","target_format":"png"}` + "\n"
		for _, step := range []struct {
			name   string
			head   string
			pieces []string
			gap    time.Duration
		}{
			{"whole", head, []string{body}, 0},
			{"eight pieces, the last at half the window", head, splitBody(body, 8), window / 16},
			{"JSON with a trailing newline in one write", requestHead(http.MethodPost, "/v1/presets", keyHeader(testClientKey), "application/json", int64(len(short))), []string{short}, 0},
			{"whole again", head, []string{body}, 0},
		} {
			r := k.earlyExchange(step.head, step.pieces, step.gap, 10*time.Second)
			t.Logf("HONEST | %s | status %d | close %v | after %v | err %v", step.name, r.status, r.close, r.elapsed.Round(time.Millisecond), r.err)
			if r.err != nil || r.status != http.StatusCreated || r.close {
				t.Fatalf("%s: status %d, close %v, err %v, want 201 on the kept connection; body %.200s", step.name, r.status, r.close, r.err, r.body)
			}
		}
		if got := news.Load(); got != 1 {
			t.Errorf("the honest client opened %d connections, want exactly 1", got)
		}
	})

	t.Run("handler outlives the window", func(t *testing.T) {
		srv := heapProbeServer()
		admin := &slowPresetAdmin{fakePresetAdmin: probePresetAdmin(), delay: 2 * window}
		srv.presetAdmin = admin
		ts := startShippedServer(t, srv.Routes())
		r := dialKept(t, ts).earlyExchange(head, []string{body}, 0, 2*window+10*time.Second)
		admin.mu.Lock()
		called, ctxErr := admin.called, admin.ctxErr
		admin.mu.Unlock()
		t.Logf("HONEST | Create sleeps %v | status %d | close %v | after %v | called %v | ctx.Err() %v | err %v", admin.delay, r.status, r.close, r.elapsed.Round(time.Millisecond), called, ctxErr, r.err)
		if r.err != nil || r.status != http.StatusCreated {
			t.Fatalf("status %d, err %v, want 201; body %.200s", r.status, r.err, r.body)
		}
		if !called || ctxErr != nil {
			t.Fatalf("Create called=%v with ctx.Err()=%v, want a live context after the whole body", called, ctxErr)
		}
	})

	t.Run("body past the window", func(t *testing.T) {
		srv := heapProbeServer()
		srv.presetAdmin = probePresetAdmin()
		ts := startShippedServer(t, srv.Routes())
		r := dialKept(t, ts).earlyExchange(head, splitBody(body, 8), window/4, 10*time.Second)
		t.Logf("HONEST | eight pieces, the last at two windows | status %d | close %v | after %v | err %v | %.200s", r.status, r.close, r.elapsed.Round(time.Millisecond), r.err, r.body)
		if r.err != nil || r.status != http.StatusBadRequest {
			t.Fatalf("status %d, err %v, want 400 for a body still incomplete at smallBodyWindow(maxPresetBodyBytes) = %v", r.status, r.err, window)
		}
		var answer struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(r.body, &answer); err != nil || answer.Code != "invalid_request" || answer.Error != "invalid JSON body" {
			t.Errorf("answer %q (decode error %v), want code invalid_request and error \"invalid JSON body\"", r.body, err)
		}
		if !r.close {
			t.Errorf("the 400 for a body past the window keeps the connection, want Connection: close")
		}
	})
}

func TestSmallBodyWindowFollowsTheHeadRate(t *testing.T) {
	var prev time.Duration
	for _, limit := range []int64{1, 512, 1024, 4096, maxPresetBodyBytes, smallBodyMaxBytes} {
		w := smallBodyWindow(limit)
		t.Logf("WINDOW | limit %d | window %v", limit, w)
		if w < refusalDrainGrace {
			t.Errorf("limit %d: window %v is shorter than refusalDrainGrace %v", limit, w, refusalDrainGrace)
		}
		if int64(w)*apiMaxHeaderBytes < limit*int64(apiReadHeaderTimeout) {
			t.Errorf("limit %d: window %v asks for more than the head rate apiMaxHeaderBytes %d per apiReadHeaderTimeout %v", limit, w, apiMaxHeaderBytes, apiReadHeaderTimeout)
		}
		if ceil := max(refusalDrainGrace, time.Duration(limit)*apiReadHeaderTimeout/apiMaxHeaderBytes) + time.Millisecond; w > ceil {
			t.Errorf("limit %d: window %v is longer than the head rate allows, want at most %v", limit, w, ceil)
		}
		if w < prev {
			t.Errorf("limit %d: window %v is shorter than %v of a smaller limit", limit, w, prev)
		}
		prev = w
	}
	if w := smallBodyWindow(smallBodyMaxBytes); w >= intakeReadDeadline {
		t.Errorf("smallBodyWindow(smallBodyMaxBytes) = %v, want shorter than intakeReadDeadline %v", w, intakeReadDeadline)
	}
	if maxPresetBodyBytes > smallBodyMaxBytes {
		t.Errorf("maxPresetBodyBytes %d is above smallBodyMaxBytes %d", maxPresetBodyBytes, smallBodyMaxBytes)
	}
}

type bodyAccess struct {
	pos     token.Pos
	fn      *ast.FuncDecl
	kind    string
	sel     *ast.SelectorExpr
	parents []ast.Node
}

type packageFuncs struct {
	fset  *token.FileSet
	info  *types.Info
	pkg   *types.Package
	decls []*ast.FuncDecl
}

func (p packageFuncs) name(d *ast.FuncDecl) string {
	if d == nil {
		return "package scope"
	}
	obj, ok := p.info.Defs[d.Name].(*types.Func)
	if !ok {
		return d.Name.Name
	}
	return strings.ReplaceAll(obj.FullName(), p.pkg.Path()+".", "")
}

func (p packageFuncs) enclosing(pos token.Pos) *ast.FuncDecl {
	for _, d := range p.decls {
		if d.Pos() <= pos && pos < d.End() {
			return d
		}
	}
	return nil
}

func (p packageFuncs) where(pos token.Pos) string {
	at := p.fset.Position(pos)
	return fmt.Sprintf("%s:%d", filepath.Base(at.Filename), at.Line)
}

func typeCheckAPIPackage(t *testing.T) (packageFuncs, []*ast.File) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-export", "-deps", "-json=ImportPath,Export,Dir,GoFiles,CgoFiles", ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export -deps: %v: %s", err, stderr.String())
	}
	type listed struct {
		ImportPath, Export, Dir string
		GoFiles, CgoFiles       []string
	}
	exports := map[string]string{}
	var self *listed
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listed
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		exports[p.ImportPath] = p.Export
		if p.Dir == dir {
			self = &p
		}
	}
	if self == nil || len(self.GoFiles) == 0 {
		t.Fatalf("go list did not report the package in %s with its Go files", dir)
	}
	if len(self.CgoFiles) > 0 {
		t.Fatalf("the package has cgo files %v the scan does not type-check", self.CgoFiles)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range self.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	lookup := func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok || export == "" {
			return nil, fmt.Errorf("go list reported no export data for %q", path)
		}
		return os.Open(export)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	pkg, err := (&types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}).Check(self.ImportPath, fset, files, info)
	if err != nil {
		t.Fatalf("type-check %s: %v", self.ImportPath, err)
	}
	p := packageFuncs{fset: fset, info: info, pkg: pkg}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				p.decls = append(p.decls, fd)
			}
		}
	}
	return p, files
}

func importedPackage(t *testing.T, pkg *types.Package, path string) *types.Package {
	t.Helper()
	for _, imp := range pkg.Imports() {
		if imp.Path() == path {
			return imp
		}
	}
	t.Fatalf("%s does not import %s", pkg.Path(), path)
	return nil
}

func namedStructField(t *testing.T, scope *types.Scope, typeName, field string) *types.Var {
	t.Helper()
	tn, ok := scope.Lookup(typeName).(*types.TypeName)
	if !ok {
		t.Fatalf("no type %s", typeName)
	}
	st, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s is not a struct", typeName)
	}
	for i := range st.NumFields() {
		if f := st.Field(i); f.Name() == field {
			return f
		}
	}
	t.Fatalf("%s has no field %s", typeName, field)
	return nil
}

func pointerMethod(t *testing.T, pkg *types.Package, typeName, method string) *types.Func {
	t.Helper()
	tn, ok := pkg.Scope().Lookup(typeName).(*types.TypeName)
	if !ok {
		t.Fatalf("no type %s in %s", typeName, pkg.Path())
	}
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, pkg, method)
	fn, ok := obj.(*types.Func)
	if !ok {
		t.Fatalf("%s.%s is not a method of *%s", pkg.Path(), method, typeName)
	}
	return fn
}

func constantFromAST(t *testing.T, info *types.Info, specs map[*types.Const]ast.Expr, e ast.Expr, depth int) constant.Value {
	t.Helper()
	if depth > 32 {
		t.Fatalf("constant expression %T nests deeper than 32", e)
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		v := constant.MakeFromLiteral(e.Value, e.Kind, 0)
		if v.Kind() == constant.Unknown {
			t.Fatalf("literal %s is not a constant", e.Value)
		}
		return v
	case *ast.Ident:
		c, ok := info.Uses[e].(*types.Const)
		value, declared := specs[c]
		if !ok || !declared {
			t.Fatalf("%s is not a constant declared with an explicit value in this package", e.Name)
		}
		return constantFromAST(t, info, specs, value, depth+1)
	case *ast.ParenExpr:
		return constantFromAST(t, info, specs, e.X, depth+1)
	case *ast.BinaryExpr:
		x := constantFromAST(t, info, specs, e.X, depth+1)
		y := constantFromAST(t, info, specs, e.Y, depth+1)
		switch e.Op {
		case token.ADD, token.SUB, token.MUL:
			return constant.BinaryOp(x, e.Op, y)
		case token.QUO:
			if x.Kind() == constant.Int && y.Kind() == constant.Int {
				return constant.BinaryOp(x, token.QUO_ASSIGN, y)
			}
			return constant.BinaryOp(x, token.QUO, y)
		case token.SHL, token.SHR:
			s, exact := constant.Uint64Val(y)
			if !exact {
				t.Fatalf("shift count %v is not a whole number", y)
			}
			return constant.Shift(x, e.Op, uint(s))
		}
		t.Fatalf("operator %s in a constant expression is not supported", e.Op)
	}
	t.Fatalf("constant expression %T is not supported", e)
	return nil
}

func TestEveryBodyReadOutsideASlotIsASmallBody(t *testing.T) {
	p, files := typeCheckAPIPackage(t)
	httpPkg := importedPackage(t, p.pkg, "net/http")
	bodyField := namedStructField(t, httpPkg.Scope(), "Request", "Body")
	formMethods := map[types.Object]string{}
	for _, m := range []string{"ParseForm", "ParseMultipartForm", "FormValue", "PostFormValue", "FormFile", "MultipartReader"} {
		formMethods[pointerMethod(t, httpPkg, "Request", m)] = m
	}
	maxBytesReader := httpPkg.Scope().Lookup("MaxBytesReader")
	noBody := httpPkg.Scope().Lookup("NoBody")
	if maxBytesReader == nil || noBody == nil {
		t.Fatal("net/http has no MaxBytesReader or NoBody")
	}
	slotOf := map[string]*types.Var{
		"(*Server).handleCreateJob": namedStructField(t, p.pkg.Scope(), "Server", "createIntakeSem"),
		"(*Server).handleInspect":   namedStructField(t, p.pkg.Scope(), "Server", "inspectIntakeSem"),
	}
	roles := map[string]string{
		"(*Server).handleCreateJob": "slot",
		"(*Server).handleInspect":   "slot",
		"parseIntakeForm":           "slot helper",
		"(*Server).newIntakeReader": "slot helper",
		"decodePresetRequest":       "small body",
		"smallBodyReader":           "small body constructor",
		"(*Server).armBodyWindow":   "window",
	}
	parseIntakeFormObj := p.pkg.Scope().Lookup("parseIntakeForm")
	newIntakeReaderObj := pointerMethod(t, p.pkg, "Server", "newIntakeReader")
	smallBodyReaderObj := p.pkg.Scope().Lookup("smallBodyReader")
	if parseIntakeFormObj == nil || smallBodyReaderObj == nil {
		t.Fatal("the package has no parseIntakeForm or smallBodyReader")
	}

	var accesses []bodyAccess
	for _, f := range files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			record := func(kind string, sel *ast.SelectorExpr) {
				accesses = append(accesses, bodyAccess{pos: n.Pos(), fn: p.enclosing(n.Pos()), kind: kind, sel: sel, parents: slices.Clone(stack)})
			}
			switch n := n.(type) {
			case *ast.SelectorExpr:
				switch obj := p.info.Uses[n.Sel]; {
				case obj == bodyField:
					record("(*net/http.Request).Body", n)
				case formMethods[obj] != "":
					record("(*net/http.Request)."+formMethods[obj], n)
				}
			case *ast.Ident:
				if p.info.Uses[n] == maxBytesReader {
					record("net/http.MaxBytesReader", nil)
				}
			}
			stack = append(stack, n)
			return true
		})
	}
	if len(accesses) == 0 {
		t.Fatal("the scan found no access to a request body in the package: it does not see the code")
	}
	sort.Slice(accesses, func(i, j int) bool { return accesses[i].pos < accesses[j].pos })

	byFunc := map[string][]bodyAccess{}
	for _, a := range accesses {
		name := p.name(a.fn)
		role, ok := roles[name]
		if !ok {
			role = "outside the allow list"
			t.Errorf("%s: %s in %s: a request body is touched outside an intake slot and outside the small-body reader", p.where(a.pos), a.kind, name)
		}
		short := "package scope"
		if a.fn != nil {
			short = a.fn.Name.Name
		}
		t.Logf("BODYREAD | %s | %s | %s | %s", p.where(a.pos), short, a.kind, role)
		byFunc[name] = append(byFunc[name], a)
	}
	for name := range roles {
		if len(byFunc[name]) == 0 {
			t.Errorf("allow-list entry %s touches no request body: the entry is stale", name)
		}
	}

	firstSend := map[string]token.Pos{}
	for _, d := range p.decls {
		name := p.name(d)
		slot, ok := slotOf[name]
		if !ok {
			continue
		}
		ast.Inspect(d, func(n ast.Node) bool {
			send, ok := n.(*ast.SendStmt)
			if !ok {
				return true
			}
			if sel, ok := send.Chan.(*ast.SelectorExpr); ok && p.info.Uses[sel.Sel] == slot {
				if at, seen := firstSend[name]; !seen || send.Pos() < at {
					firstSend[name] = send.Pos()
				}
			}
			return true
		})
	}
	for name := range slotOf {
		at, ok := firstSend[name]
		if !ok {
			t.Errorf("%s sends to no %s intake slot", name, slotOf[name].Name())
			continue
		}
		if got := byFunc[name]; len(got) > 0 && got[0].pos < at {
			t.Errorf("%s: %s in %s comes before the send to %s at %s: the body is touched before the intake slot is taken", p.where(got[0].pos), got[0].kind, name, slotOf[name].Name(), p.where(at))
		}
	}

	for _, helper := range []types.Object{parseIntakeFormObj, newIntakeReaderObj} {
		uses := 0
		for id, obj := range p.info.Uses {
			if obj != helper {
				continue
			}
			uses++
			fn := p.name(p.enclosing(id.Pos()))
			at, ok := firstSend[fn]
			if roles[fn] != "slot" || !ok || id.Pos() < at {
				t.Errorf("%s: %s is used in %s outside an intake slot", p.where(id.Pos()), helper.Name(), fn)
			}
		}
		if uses < 2 {
			t.Errorf("%s is used %d times, want at least 2: one in each intake slot", helper.Name(), uses)
		}
	}

	specs := map[*types.Const]ast.Expr{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if c, ok := p.info.Defs[name].(*types.Const); ok && i < len(vs.Values) {
						specs[c] = vs.Values[i]
					}
				}
			}
		}
	}
	decode := byFunc["decodePresetRequest"]
	if len(decode) > 0 {
		first := decode[0]
		ok := false
		var limit int64
		var assign *ast.AssignStmt
		if len(first.parents) > 0 {
			assign, _ = first.parents[len(first.parents)-1].(*ast.AssignStmt)
		}
		if first.sel != nil && first.kind == "(*net/http.Request).Body" && assign != nil && assign.Tok == token.ASSIGN && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 && assign.Lhs[0] == first.sel {
			call, isCall := assign.Rhs[0].(*ast.CallExpr)
			if isCall && len(call.Args) == 3 {
				fun, _ := call.Fun.(*ast.Ident)
				w, _ := call.Args[0].(*ast.Ident)
				body, _ := call.Args[1].(*ast.SelectorExpr)
				k, _ := call.Args[2].(*ast.Ident)
				recv, _ := first.sel.X.(*ast.Ident)
				params := first.fn.Type.Params.List
				var wParam types.Object
				if len(params) > 0 && len(params[0].Names) > 0 {
					wParam = p.info.Defs[params[0].Names[0]]
				}
				if fun != nil && p.info.Uses[fun] == smallBodyReaderObj && w != nil && wParam != nil && p.info.Uses[w] == wParam && body != nil && p.info.Uses[body.Sel] == bodyField && recv != nil {
					bodyRecv, _ := body.X.(*ast.Ident)
					c, isConst := p.info.Uses[k].(*types.Const)
					if bodyRecv != nil && p.info.Uses[bodyRecv] == p.info.Uses[recv] && isConst && c.Pkg() == p.pkg && c.Parent() == p.pkg.Scope() {
						v := constant.ToInt(constantFromAST(t, p.info, specs, k, 0))
						n, exact := constant.Int64Val(v)
						if !exact {
							t.Fatalf("%s = %v is not an int64", k.Name, v)
						}
						if typed := p.info.Types[k].Value; typed != nil && constant.Compare(typed, token.NEQ, v) {
							t.Fatalf("%s: the AST gives %v, the type checker %v", k.Name, v, typed)
						}
						limit, ok = n, true
						t.Logf("BODYREAD | %s | decodePresetRequest | smallBodyReader cap %s = %d | small body (smallBodyMaxBytes %d)", p.where(call.Pos()), k.Name, n, smallBodyMaxBytes)
					}
				}
			}
		}
		if !ok {
			t.Errorf("%s: the first body access of decodePresetRequest is not r.Body = smallBodyReader(w, r.Body, K) with K a package constant", p.where(first.pos))
		} else if limit > smallBodyMaxBytes {
			t.Errorf("decodePresetRequest caps its body at %d bytes, above smallBodyMaxBytes %d: the short window does not cover it", limit, smallBodyMaxBytes)
		}
	}

	smallUses := 0
	for id, obj := range p.info.Uses {
		if obj != smallBodyReaderObj {
			continue
		}
		smallUses++
		if fn := p.name(p.enclosing(id.Pos())); roles[fn] != "small body" {
			t.Errorf("%s: smallBodyReader is used in %s, outside the small-body readers", p.where(id.Pos()), fn)
		}
	}
	if smallUses == 0 {
		t.Errorf("smallBodyReader is used nowhere")
	}

	for _, name := range []string{"smallBodyReader", "(*Server).newIntakeReader"} {
		limits := 0
		for _, a := range byFunc[name] {
			if a.kind == "net/http.MaxBytesReader" {
				limits++
				continue
			}
			t.Errorf("%s: %s in %s, want only the body-limit constructor", p.where(a.pos), a.kind, name)
		}
		if limits != 1 {
			t.Errorf("%s references net/http.MaxBytesReader %d times, want exactly 1", name, limits)
		}
	}

	isNoBodyOrNil := func(e ast.Expr) bool {
		switch e := e.(type) {
		case *ast.Ident:
			_, isNil := p.info.Uses[e].(*types.Nil)
			return isNil
		case *ast.SelectorExpr:
			return p.info.Uses[e.Sel] == noBody
		}
		return false
	}
	for _, a := range byFunc["(*Server).armBodyWindow"] {
		ok := false
		if a.kind == "(*net/http.Request).Body" && len(a.parents) > 0 {
			switch parent := a.parents[len(a.parents)-1].(type) {
			case *ast.BinaryExpr:
				ok = parent.Op == token.EQL && ((parent.X == ast.Expr(a.sel) && isNoBodyOrNil(parent.Y)) || (parent.Y == ast.Expr(a.sel) && isNoBodyOrNil(parent.X)))
			case *ast.KeyValueExpr:
				if len(a.parents) > 1 {
					_, inLit := a.parents[len(a.parents)-2].(*ast.CompositeLit)
					ok = inLit && parent.Value == ast.Expr(a.sel)
				}
			case *ast.CompositeLit:
				ok = true
			case *ast.AssignStmt:
				ok = slices.Contains(parent.Lhs, ast.Expr(a.sel))
			}
		}
		if !ok {
			t.Errorf("%s: %s in armBodyWindow does more than compare the body with nil or http.NoBody, put it in a composite literal or assign it", p.where(a.pos), a.kind)
		}
	}
}

func TestSmallBodyRuleIsStatedInTheContracts(t *testing.T) {
	ms := smallBodyWindow(maxPresetBodyBytes).Milliseconds()
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(raw), "**Status semantics**")
	if !ok {
		t.Fatal("README.md has no Status semantics section")
	}
	section, _, _ = strings.Cut(section, "\n**")
	_, item, ok := strings.Cut(section, "\n- A JSON request body")
	if !ok {
		t.Errorf("README.md Status semantics has no item that starts with %q", "A JSON request body")
	} else {
		item, _, _ = strings.Cut(item, "\n- ")
		flat := "A JSON request body" + strings.Join(strings.Fields(item), " ")
		for _, phrase := range []string{fmt.Sprintf("within %d ms", ms), "the connection closes"} {
			if !strings.Contains(flat, phrase) {
				t.Errorf("README.md Status semantics, item %q, does not say %q", flat, phrase)
			}
		}
	}

	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("decode %s: %v", specPath, err)
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no paths mapping", specPath)
	}
	var found []string
	for path, item := range paths {
		methods, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for method, op := range methods {
			operation, ok := op.(map[string]any)
			if !ok {
				continue
			}
			body, ok := operation["requestBody"].(map[string]any)
			if !ok {
				continue
			}
			content, ok := body["content"].(map[string]any)
			if !ok {
				continue
			}
			if _, ok := content["application/json"]; !ok {
				continue
			}
			found = append(found, strings.ToUpper(method)+" "+path)
			description := openAPIString(t, doc, "paths", path, method, "responses", "400", "description")
			for _, want := range []string{
				fmt.Sprintf("The JSON body has %d ms to arrive in full", ms),
				"answered with this 400 (invalid_request, invalid JSON body)",
			} {
				if !strings.Contains(description, want) {
					t.Errorf("%s: the 400 of %s %s does not state %q; it reads %q", specPath, strings.ToUpper(method), path, want, description)
				}
			}
		}
	}
	sort.Strings(found)
	t.Logf("operations with an application/json request body: %v", found)
	if len(found) == 0 {
		t.Errorf("%s has no operation with an application/json request body", specPath)
	}
}
