// tools/coderef/main.go — Hijarr code reference generator.
// Usage: CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md
//
// Reads all non-test .go files, extracts exported symbols via go/ast,
// and scans for SQL tables and Gin routes via regex, then renders
// docs/CODEREF.md — a dense, LLM-friendly symbol index.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ── Types ─────────────────────────────────────────────────────────────────────

type SymKind string

const (
	KindFunc   SymKind = "F"
	KindMethod SymKind = "M"
	KindType   SymKind = "T"
	KindVar    SymKind = "V"
	KindConst  SymKind = "C"
)

type Symbol struct {
	Kind      SymKind
	PkgPath   string // e.g. "internal/cache"
	File      string // e.g. "subtitle_cache.go"
	Line      int
	Name      string // e.g. "GetSubtitleCache"
	Receiver  string // e.g. "*SubtitleCache" (empty for funcs)
	Signature string // e.g. "(dbPath string) *SubtitleCache"
	Doc       string // first sentence of doc comment
}

type RouteEntry struct {
	Method  string
	Path    string
	Handler string
	File    string
	Line    int
}

type TableEntry struct {
	Name string
	File string
	Line int
}

type FileInfo struct {
	PkgPath string
	File    string
	Lines   int
	Brief   string // short description from package doc or first comment
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	var symbols []Symbol
	var routes []RouteEntry
	var tables []TableEntry
	var fileInfos []FileInfo
	importGraph := map[string][]string{} // pkgPath → []imported internal pkgs

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// skip hidden dirs, vendor, tools itself
		if d.IsDir() {
			name := d.Name()
			if name == "." {
				return nil
			}
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "tools" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		syms, fi, imps, err2 := parseFile(root, path)
		if err2 != nil {
			fmt.Fprintf(os.Stderr, "warn: parse %s: %v\n", path, err2)
			return nil
		}
		symbols = append(symbols, syms...)
		fileInfos = append(fileInfos, fi)
		for _, imp := range imps {
			importGraph[fi.PkgPath] = appendUniq(importGraph[fi.PkgPath], imp)
		}

		r, t := scanLines(root, path)
		routes = append(routes, r...)
		tables = append(tables, t...)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk error: %v\n", err)
		os.Exit(1)
	}

	renderCODEREF(symbols, routes, tables, fileInfos, importGraph)
}

// ── File Parser ───────────────────────────────────────────────────────────────

func parseFile(root, path string) ([]Symbol, FileInfo, []string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, FileInfo{}, nil, err
	}

	// Compute relative pkg path (strip root prefix and filename)
	rel, _ := filepath.Rel(root, path)
	pkgPath := filepath.ToSlash(filepath.Dir(rel))
	if pkgPath == "." {
		pkgPath = "."
	}
	fileName := filepath.Base(path)

	// Count lines
	lineCount := countLines(path)

	// Brief from package doc comment
	brief := ""
	if node.Doc != nil && len(node.Doc.List) > 0 {
		brief = docFirstSentence(node.Doc.List[0].Text)
	}

	fi := FileInfo{
		PkgPath: pkgPath,
		File:    fileName,
		Lines:   lineCount,
		Brief:   brief,
	}

	// Extract internal imports
	var internalImps []string
	for _, imp := range node.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(impPath, "hijarr/internal/") {
			short := strings.TrimPrefix(impPath, "hijarr/")
			internalImps = appendUniq(internalImps, short)
		}
	}

	// Extract symbols
	var syms []Symbol
	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !ast.IsExported(d.Name.Name) {
				continue
			}
			sym := Symbol{
				PkgPath: pkgPath,
				File:    fileName,
				Line:    fset.Position(d.Pos()).Line,
				Name:    d.Name.Name,
				Doc:     extractDoc(d.Doc),
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sym.Kind = KindMethod
				sym.Receiver = formatExpr(d.Recv.List[0].Type)
			} else {
				sym.Kind = KindFunc
			}
			sym.Signature = formatFuncSignature(d.Type)
			syms = append(syms, sym)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !ast.IsExported(s.Name.Name) {
						continue
					}
					sym := Symbol{
						Kind:    KindType,
						PkgPath: pkgPath,
						File:    fileName,
						Line:    fset.Position(s.Pos()).Line,
						Name:    s.Name.Name,
						Doc:     extractDoc(d.Doc),
					}
					sym.Signature = formatTypeBody(s.Type)
					syms = append(syms, sym)

				case *ast.ValueSpec:
					kind := KindVar
					if d.Tok == token.CONST {
						kind = KindConst
					}
					for _, name := range s.Names {
						if !ast.IsExported(name.Name) {
							continue
						}
						typStr := ""
						if s.Type != nil {
							typStr = " " + formatExpr(s.Type)
						}
						sym := Symbol{
							Kind:      kind,
							PkgPath:   pkgPath,
							File:      fileName,
							Line:      fset.Position(s.Pos()).Line,
							Name:      name.Name,
							Signature: typStr,
							Doc:       extractDoc(d.Doc),
						}
						syms = append(syms, sym)
					}
				}
			}
		}
	}

	return syms, fi, internalImps, nil
}

// ── Expression Formatter ──────────────────────────────────────────────────────

func formatExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + formatExpr(e.X)
	case *ast.SelectorExpr:
		// Drop package prefix for brevity: url.Values → Values
		return formatExpr(e.Sel)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + formatExpr(e.Elt)
		}
		return "[...]" + formatExpr(e.Elt)
	case *ast.MapType:
		return "map[" + formatExpr(e.Key) + "]" + formatExpr(e.Value)
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + formatExpr(e.Value)
		case ast.RECV:
			return "<-chan " + formatExpr(e.Value)
		default:
			return "chan " + formatExpr(e.Value)
		}
	case *ast.FuncType:
		return "func" + formatFuncSignature(e)
	case *ast.InterfaceType:
		return "interface{...}"
	case *ast.StructType:
		return "struct{...}"
	case *ast.Ellipsis:
		return "..." + formatExpr(e.Elt)
	case *ast.IndexExpr:
		return formatExpr(e.X) + "[" + formatExpr(e.Index) + "]"
	default:
		return "?"
	}
}

func formatFuncSignature(ft *ast.FuncType) string {
	params := formatFieldList(ft.Params, true)
	results := ""
	if ft.Results != nil && len(ft.Results.List) > 0 {
		r := formatFieldList(ft.Results, false)
		if len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) == 0 {
			results = " " + r
		} else {
			results = " (" + r + ")"
		}
	}
	return "(" + params + ")" + results
}

func formatFieldList(fl *ast.FieldList, dropNames bool) string {
	if fl == nil {
		return ""
	}
	var parts []string
	for _, f := range fl.List {
		typStr := formatExpr(f.Type)
		if dropNames || len(f.Names) == 0 {
			parts = append(parts, typStr)
		} else if len(f.Names) == 1 {
			parts = append(parts, typStr)
		} else {
			parts = append(parts, fmt.Sprintf("%s×%d", typStr, len(f.Names)))
		}
	}
	sig := strings.Join(parts, ",")
	if len(sig) > 65 {
		sig = sig[:62] + "..."
	}
	return sig
}

func formatTypeBody(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StructType:
		return formatStructFields(t)
	case *ast.InterfaceType:
		return formatInterfaceMethods(t)
	default:
		return formatExpr(expr)
	}
}

func formatStructFields(st *ast.StructType) string {
	if st.Fields == nil || len(st.Fields.List) == 0 {
		return "{}"
	}
	var fields []string
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			// embedded
			fields = append(fields, formatExpr(f.Type))
			continue
		}
		for _, name := range f.Names {
			if ast.IsExported(name.Name) {
				fields = append(fields, name.Name)
			}
		}
	}
	if len(fields) == 0 {
		return "{unexported}"
	}
	body := "{" + strings.Join(fields, ",") + "}"
	if len(body) > 70 {
		body = "{" + strings.Join(fields[:min(5, len(fields))], ",") + ",...}"
	}
	return body
}

func formatInterfaceMethods(it *ast.InterfaceType) string {
	if it.Methods == nil || len(it.Methods.List) == 0 {
		return "{}"
	}
	var methods []string
	for _, m := range it.Methods.List {
		if len(m.Names) > 0 {
			methods = append(methods, m.Names[0].Name+"()")
		} else {
			methods = append(methods, formatExpr(m.Type))
		}
	}
	return "{" + strings.Join(methods, ";") + "}"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── Doc Comment Extraction ────────────────────────────────────────────────────

func extractDoc(cg *ast.CommentGroup) string {
	if cg == nil || len(cg.List) == 0 {
		return ""
	}
	text := cg.List[0].Text
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimPrefix(text, " ")
	return docFirstSentence(text)
}

func docFirstSentence(s string) string {
	s = strings.TrimPrefix(s, "//")
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, ". "); idx != -1 {
		s = s[:idx+1]
	}
	if len(s) > 72 {
		s = s[:69] + "..."
	}
	return s
}

// ── Line Scanner (regex-based) ────────────────────────────────────────────────

var (
	reCreateTable = regexp.MustCompile(`(?i)CREATE TABLE(?:\s+IF NOT EXISTS)?\s+(\w+)`)
	reGinRoute    = regexp.MustCompile(`\b(GET|POST|PUT|DELETE|PATCH|NoRoute|Any)\s*\(\s*"([^"]*)"`)
	reGinNoRoute  = regexp.MustCompile(`\.NoRoute\(`)
)

func scanLines(root, path string) ([]RouteEntry, []TableEntry) {
	rel, _ := filepath.Rel(root, path)
	shortPath := filepath.ToSlash(rel)

	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var routes []RouteEntry
	var tables []TableEntry

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if m := reCreateTable.FindStringSubmatch(line); m != nil {
			tables = append(tables, TableEntry{Name: m[1], File: shortPath, Line: lineNum})
		}

		if m := reGinRoute.FindStringSubmatch(line); m != nil {
			routes = append(routes, RouteEntry{
				Method: m[1],
				Path:   m[2],
				File:   shortPath,
				Line:   lineNum,
			})
		} else if reGinNoRoute.MatchString(line) {
			routes = append(routes, RouteEntry{
				Method: "NoRoute",
				Path:   "(catch-all)",
				File:   shortPath,
				Line:   lineNum,
			})
		}
	}
	return routes, tables
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		n++
	}
	return n
}

func appendUniq(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

// ── Renderer ──────────────────────────────────────────────────────────────────

func renderCODEREF(
	symbols []Symbol,
	routes []RouteEntry,
	tables []TableEntry,
	fileInfos []FileInfo,
	importGraph map[string][]string,
) {
	w := os.Stdout

	totalFiles := len(fileInfos)
	totalLines := 0
	for _, fi := range fileInfos {
		totalLines += fi.Lines
	}

	// §1 Meta
	fmt.Fprintf(w, "<!-- CODEREF — generated %s by tools/coderef/main.go -->\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "<!-- Files: %d | Lines: %d -->\n", totalFiles, totalLines)
	fmt.Fprintf(w, "<!-- Regen: CGO_ENABLED=0 go run ./tools/coderef > docs/CODEREF.md -->\n")
	fmt.Fprintf(w, "<!-- Reading tip: §5 Symbol Index is primary lookup. §3=routes §4=DB §7=deps -->\n")
	fmt.Fprintln(w)

	// §2 Package Tree
	fmt.Fprintln(w, "## §2 Package Tree")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| pkg | file | L | notes |")
	fmt.Fprintln(w, "|-----|------|---|-------|")

	sort.Slice(fileInfos, func(i, j int) bool {
		if fileInfos[i].PkgPath != fileInfos[j].PkgPath {
			return fileInfos[i].PkgPath < fileInfos[j].PkgPath
		}
		return fileInfos[i].File < fileInfos[j].File
	})
	for _, fi := range fileInfos {
		fmt.Fprintf(w, "| `%s` | %s | %d | %s |\n", fi.PkgPath, fi.File, fi.Lines, fi.Brief)
	}
	fmt.Fprintln(w)

	// §3 HTTP Routes
	fmt.Fprintln(w, "## §3 HTTP Routes  [server :8001]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w, "Dispatch priority — NoRoute catch-all (cmd/hijarr/main.go:101):")
	fmt.Fprintln(w, "  1. path HasPrefix /debug/        → debug.HandleDebug")
	fmt.Fprintln(w, "  2. path == /assrt-dl             → proxy.AssrtFileProxy")
	fmt.Fprintln(w, "  3. host Contains api.assrt.net   → proxy.AssrtMitmProxy")
	fmt.Fprintln(w, "  4. path HasPrefix /api           → proxy.TorznabProxy")
	fmt.Fprintln(w, "  5. fallthrough                   → proxy.TVDBMitmProxy")
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Static routes (internal/web/api.go):")
	fmt.Fprintln(w)

	// Deduplicate and sort routes (exclude NoRoute catch-all dupes)
	seenRoutes := map[string]bool{}
	var staticRoutes []RouteEntry
	for _, r := range routes {
		key := r.Method + "|" + r.Path + "|" + r.File
		if seenRoutes[key] {
			continue
		}
		seenRoutes[key] = true
		if r.Method == "NoRoute" && r.Path == "(catch-all)" {
			continue // already rendered above
		}
		staticRoutes = append(staticRoutes, r)
	}
	sort.Slice(staticRoutes, func(i, j int) bool {
		return staticRoutes[i].File+staticRoutes[i].Path < staticRoutes[j].File+staticRoutes[j].Path
	})

	fmt.Fprintln(w, "| Method | Path | File:L |")
	fmt.Fprintln(w, "|--------|------|--------|")
	for _, r := range staticRoutes {
		fmt.Fprintf(w, "| %s | `%s` | %s:%d |\n", r.Method, r.Path, r.File, r.Line)
	}

	// Debug endpoints hardcoded (too many dynamic ones from scan)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Debug endpoints (internal/debug/handler.go) — prefix `/debug/`:")
	fmt.Fprintln(w, "```")
	debugEndpoints := []string{
		"GET  /debug/srn/stats          — srn_events+subtitle_cache+seen_files行数",
		"GET  /debug/srn/events         — srn_events列表(无BLOB); ?title= ?season= ?ep=",
		"GET  /debug/srn/cache          — subtitle_cache条目",
		"GET  /debug/srn/rss            — seen_rss GUIDs",
		"GET  /debug/srn/dump           — 全量JSON",
		"GET  /debug/srn/seen-files     — seen_files; ?path= filter",
		"GET  /debug/srn/failed-files   — failed_files列表",
		"POST /debug/srn/correct/delete-event      — 删除单个srn_events行",
		"POST /debug/srn/correct/delete-title      — 按title批量删除",
		"POST /debug/srn/correct/reingest          — 重新入库(先ForgetFile)",
		"POST /debug/srn/correct/delete-failed-file — 从failed_files移除",
		"POST /debug/srn/correct/delete-seen-file  — 从seen_files移除",
		"POST /debug/srn/correct/delete-rss        — 从seen_rss移除",
		"POST /debug/srn/correct/delete-cache      — 从subtitle_cache删除",
	}
	for _, ep := range debugEndpoints {
		fmt.Fprintln(w, "  "+ep)
	}
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)

	// §4 DB Schema
	fmt.Fprintln(w, "## §4 DB Schema  [SQLite WAL — modernc.org/sqlite, all via db.Open()]")
	fmt.Fprintln(w)

	// Deduplicate tables
	seenTables := map[string]bool{}
	var uniqTables []TableEntry
	for _, t := range tables {
		if seenTables[t.Name] {
			continue
		}
		seenTables[t.Name] = true
		uniqTables = append(uniqTables, t)
	}
	sort.Slice(uniqTables, func(i, j int) bool { return uniqTables[i].Name < uniqTables[j].Name })

	for _, t := range uniqTables {
		fmt.Fprintf(w, "**%s** — %s:%d\n\n", t.Name, t.File, t.Line)
	}

	// Hardcoded schema details (authoritative version)
	fmt.Fprintln(w, "```")
	schemas := []string{
		"srn_events(id TEXT PK, kind INT=1001, created_at INT, tags TEXT=JSON, filename TEXT,",
		"           title TEXT, tmdb_id TEXT, lang TEXT='zh', season INT, ep INT, content BLOB)",
		"  idx: (title,season,ep)  (tmdb_id,season,ep)",
		"seen_files(path TEXT PK, mtime_ns INT)",
		"failed_files(path TEXT PK, failed_at INT)  — TTL via DISK_SCAN_FAIL_TTL",
		"global_stats(key TEXT PK, value INT=0)",
		"tried_magnets(hash TEXT PK, tried_at INT)",
		"metadata_cache(raw_title TEXT PK, tmdb_id INT, title TEXT, season INT, episode INT,",
		"               aliases TEXT=JSON, created_at INT, updated_at INT)",
		"subtitle_cache(key TEXT PK='title|Sn|En', value TEXT=JSON, cached_at INT, updated_at INT)",
		"torrent_patterns(source_type TEXT, source_key TEXT, tmdb_id INT, regex_config TEXT=JSON,",
		"                 original_name TEXT, updated_at INT — PK(source_type,source_key))",
		"subtitle_archives(file_md5 TEXT PK, parent_source_type TEXT, parent_source_key TEXT,",
		"                  content_map TEXT=JSON, updated_at INT)",
		"seen_rss(guid TEXT PK)  — created lazily when RSS_FEEDS configured",
	}
	for _, s := range schemas {
		fmt.Fprintln(w, s)
	}
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)

	// §5 Symbol Index
	fmt.Fprintln(w, "## §5 Symbol Index")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Format: `KIND  Signature  file:LINE  // doc`")
	fmt.Fprintln(w, "KIND: F=func M=method T=type I=iface V=var C=const")
	fmt.Fprintln(w)

	// Group by package
	pkgSyms := map[string][]Symbol{}
	pkgOrder := []string{}
	for _, sym := range symbols {
		if _, ok := pkgSyms[sym.PkgPath]; !ok {
			pkgOrder = append(pkgOrder, sym.PkgPath)
		}
		pkgSyms[sym.PkgPath] = append(pkgSyms[sym.PkgPath], sym)
	}
	sort.Strings(pkgOrder)

	for _, pkg := range pkgOrder {
		syms := pkgSyms[pkg]
		// Sort by file then line
		sort.Slice(syms, func(i, j int) bool {
			if syms[i].File != syms[j].File {
				return syms[i].File < syms[j].File
			}
			return syms[i].Line < syms[j].Line
		})

		fmt.Fprintf(w, "### pkg: `%s`\n", pkg)
		for _, sym := range syms {
			renderSymbol(w, sym)
		}
		fmt.Fprintln(w)
	}

	// §6 Config Index
	fmt.Fprintln(w, "## §6 Config Index  (internal/config/config.go)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Var | Env | Default |")
	fmt.Fprintln(w, "|-----|-----|---------|")
	configVars := [][]string{
		{"TMDBAPIKey", "TMDB_API_KEY", `""`},
		{"TargetLanguage", "TARGET_LANGUAGE", "zh-CN"},
		{"TVDBLanguage", "TVDB_LANGUAGE", "zho"},
		{"AssrtAPIToken", "ASSRT_API_TOKEN", `""`},
		{"AssrtRateLimit float64", "ASSRT_RATE_LIMIT", "2.0"},
		{"SubtitleSearchTimeout Duration", "SUBTITLE_SEARCH_TIMEOUT", "3s"},
		{"CacheDBPath", "CACHE_DB_PATH", "/data/hijarr.db"},
		{"LocalDownloadPaths", "LOCAL_DOWNLOAD_PATHS", `"" (CSV)`},
		{"DiskScanInterval Duration", "DISK_SCAN_INTERVAL", "0=disabled"},
		{"DiskScanLogDir", "DISK_SCAN_LOG_DIR", "/data/logs"},
		{"DiskScanFailTTL Duration", "DISK_SCAN_FAIL_TTL", "168h"},
		{"QBitURL/User/Pass", "QBIT_URL/USER/PASS", `""`},
		{"QBitMaxJobs int", "QBIT_MAX_JOBS", "2"},
		{"RSSFeeds []string", "RSS_FEEDS", `"" (CSV)`},
		{"RSSFeedInterval Duration", "RSS_SCAN_INTERVAL", "1h"},
		{"SonarrURL/APIKey", "SONARR_URL/API_KEY", `""`},
		{"SonarrSyncInterval Duration", "SONARR_SYNC_INTERVAL", "5m"},
		{"SonarrPathPrefix/LocalPathPrefix", "SONARR/LOCAL_PATH_PREFIX", `""`},
		{"LLMAPIKey/BaseURL/Model", "LLM_API_KEY/BASE_URL/MODEL", "openai defaults"},
		{"ProwlarrTargetURL/APIKey", "PROWLARR_TARGET_URL/API_KEY", "prowlarr:9696"},
	}
	for _, cv := range configVars {
		fmt.Fprintf(w, "| `%s` | `%s` | `%s` |\n", cv[0], cv[1], cv[2])
	}
	fmt.Fprintln(w)

	// §7 Dep Graph
	fmt.Fprintln(w, "## §7 Package Dependency Graph  (internal/* imports only)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	pkgs := make([]string, 0, len(importGraph))
	for k := range importGraph {
		pkgs = append(pkgs, k)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		deps := importGraph[pkg]
		sort.Strings(deps)
		if len(deps) > 0 {
			fmt.Fprintf(w, "%-40s → %s\n", pkg, strings.Join(deps, ", "))
		}
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Leaf pkgs (no internal deps): db, util, config, logger, metrics")
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)

	// §8 Patterns
	renderPatterns(w)

	// §9 Constraints
	renderConstraints(w)

	// §10 Changelog
	fmt.Fprintln(w, "## §10 Changelog")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Date | Op | Detail |")
	fmt.Fprintln(w, "|------|----|--------|")
	fmt.Fprintf(w, "| %s | REGEN | %d files %dL |\n",
		time.Now().Format("2006-01-02"), totalFiles, totalLines)
}

func renderSymbol(w *os.File, sym Symbol) {
	doc := ""
	if sym.Doc != "" {
		doc = "  // " + sym.Doc
	}
	switch sym.Kind {
	case KindMethod:
		fmt.Fprintf(w, "M  (%s) %s%s  %s:%d%s\n",
			sym.Receiver, sym.Name, sym.Signature, sym.File, sym.Line, doc)
	case KindFunc:
		fmt.Fprintf(w, "F  %s%s  %s:%d%s\n",
			sym.Name, sym.Signature, sym.File, sym.Line, doc)
	case KindType:
		fmt.Fprintf(w, "T  %s%s  %s:%d%s\n",
			sym.Name, sym.Signature, sym.File, sym.Line, doc)
	case KindVar:
		fmt.Fprintf(w, "V  %s%s  %s:%d%s\n",
			sym.Name, sym.Signature, sym.File, sym.Line, doc)
	case KindConst:
		fmt.Fprintf(w, "C  %s%s  %s:%d%s\n",
			sym.Name, sym.Signature, sym.File, sym.Line, doc)
	}
}

// ── Hardcoded Sections ────────────────────────────────────────────────────────

func renderPatterns(w *os.File) {
	fmt.Fprintln(w, "## §8 Patterns & Architecture")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	patterns := []string{
		"SINGLETON (sync.Once):",
		"  cache.GetSubtitleCache(dbPath) → *SubtitleCache   [subtitle_cache.go]",
		"  cache.GetMetadataCache(dbPath) → *MetadataCache   [metadata_cache.go]",
		"  srn.GetStore(dbPath)           → *Store           [store.go]",
		"  All three share the SAME SQLite file (CACHE_DB_PATH).",
		"",
		"INTERFACE IMPLEMENTORS:",
		"  subtitles.SubtitleProvider  ← AssrtProvider, CircuitBreakerProvider,",
		"                                 SRNProvider, ProwlarrProvider",
		"  subtitles.SemanticProvider  ← SRNProvider, ProwlarrProvider",
		"  scheduler.Job               ← DiskScanJob, RSSFeedJob, SonarrSyncJob",
		"  scheduler.Triggerable       ← DiskScanJob (only)",
		"",
		"INJECTION WIRING (cmd/hijarr/main.go):",
		"  proxy.OnLocalMiss        = diskJob.Trigger          // SRN miss → disk scan",
		"  debug.SetDiskJob(diskJob)                           // /correct/reingest → ForgetFile",
		"  proxy.AppendProvider(NewProwlarrProvider(rssJob))   // cache miss → Prowlarr async",
		"",
		"SUBTITLE REQUEST PIPELINE:",
		"  Bazarr → AssrtMitmProxy → parseAndTranslateQuery → BuildQueries",
		"    → AggregateSearch[SRNProvider, CircuitBreakerProvider(Assrt), ProwlarrProvider?]",
		"    → FeedSync(download+unpack+Publish) → WashSubtitles → JSON",
		"",
		"inFlight sync.Map RULE:",
		"  Used in srn/feeder.go for concurrent download dedup.",
		"  MUST Delete() immediately after use in loop — NOT with defer.",
		"",
		"CACHE KEY FORMAT:",
		"  subtitle_cache key = BuildCacheKey(title,season,ep) = 'title|S{n}|E{n}'",
		"  srn_events query: (tmdb_id,season,ep) primary; fallback (title,season,ep)",
	}
	for _, p := range patterns {
		fmt.Fprintln(w, p)
	}
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
}

func renderConstraints(w *os.File) {
	fmt.Fprintln(w, "## §9 Hard Constraints (NEVER violate)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "```")
	constraints := []string{
		"1. CGO_ENABLED=0 always — Intel ICC compiler incompatibility",
		"2. No static Gin routes alongside /*path — causes panic",
		"   (use NoRoute catch-all, dispatch by path inside handler)",
		"3. No defer inside for-loop to release inFlight sync.Map keys — goroutine leak",
		"4. Do NOT cache empty or unvalidated subtitle results",
		"5. Do NOT trigger Assrt circuit breaker from FeedSync/qBit download failures",
		"   (only CircuitBreakerProvider should call subtitles/status.SetAssrtBackoff)",
		"6. DiskScanJob: skip files > 50MB (maxDiskScanFileBytes cap — OOM prevention)",
		"7. All SQLite via db.Open() — not sql.Open(\"sqlite\",...) directly",
		"8. modernc.org/sqlite is pure Go — do not add CGO sqlite drivers",
		"9. Do not register new Gin routes outside web.RegisterRoutes()",
		"10. SonarrSyncInterval minimum = 5m to avoid Sonarr API hammering",
	}
	for _, c := range constraints {
		fmt.Fprintln(w, c)
	}
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
}
