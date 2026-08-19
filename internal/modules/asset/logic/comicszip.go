package logic

import (
	"archive/zip"
	"encoding/json"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	reLeadingNum = regexp.MustCompile(`^(\d+)`)
	reHua        = regexp.MustCompile(`第\s*(\d+)\s*话`)
	rePageNum    = regexp.MustCompile(`(?i)page[_\-]?(\d+)`)
	imageExt     = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
	}
	coverNames = map[string]bool{"cover": true, "folder": true, "thumb": true}
)

type zipEntry struct {
	Path string
	File *zip.File
}

type parsedPage struct {
	Name string
	File *zip.File
}

type parsedChapter struct {
	Seq   int
	Title string
	Dir   string
	Pages []parsedPage
}

type parsedManga struct {
	Root     string
	Title    string
	Intro    string
	Author   string
	Category string
	Cover    *zip.File
	CoverExt string
	Chapters []parsedChapter
}

func parseComicsZip(zr *zip.Reader) ([]parsedManga, error) {
	var entries []zipEntry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		p := strings.TrimLeft(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		if p == "" || isJunkPath(p) {
			continue
		}
		entries = append(entries, zipEntry{Path: p, File: f})
	}
	if len(entries) == 0 {
		return nil, errZip("压缩包里没有可用文件")
	}

	base := commonUnwrap(entries)
	if looksLikeManga(base, entries) {
		m, err := parseOneManga(base, entries)
		if err != nil {
			return nil, err
		}
		return []parsedManga{m}, nil
	}

	roots := uniqueFirstSeg(base, entries)
	out := make([]parsedManga, 0, len(roots))
	for _, root := range roots {
		prefix := joinPrefix(base, root)
		if !looksLikeManga(prefix, entries) {
			continue
		}
		m, err := parseOneManga(prefix, entries)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, errZip("未识别到漫画目录：需要封面/说明，以及章节文件夹里的页图")
	}
	if len(out) > 30 {
		return nil, errZip("单个压缩包最多 30 部漫画")
	}
	return out, nil
}

type zipErr string

func (e zipErr) Error() string { return string(e) }
func errZip(s string) error    { return zipErr(s) }

func isJunkPath(p string) bool {
	low := strings.ToLower(p)
	if strings.Contains(low, "__macosx/") || strings.HasSuffix(low, "/.ds_store") || strings.HasSuffix(low, ".ds_store") {
		return true
	}
	base := strings.ToLower(path.Base(p))
	return base == ".keep" || base == "thumbs.db"
}

func commonUnwrap(entries []zipEntry) string {
	cur := ""
	for {
		segs := uniqueFirstSeg(cur, entries)
		if len(segs) != 1 {
			return cur
		}
		next := joinPrefix(cur, segs[0])
		if looksLikeManga(next, entries) {
			return next
		}
		// 只有一层包装目录时剥掉, 避免误把章节目录当成包装
		if hasDirectImages(next, entries) || hasChapterDirs(next, entries) {
			return next
		}
		cur = next
		if strings.Count(cur, "/") >= 2 {
			return cur
		}
	}
}

func uniqueFirstSeg(prefix string, entries []zipEntry) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, e := range entries {
		rel := trimPrefix(e.Path, prefix)
		if rel == "" {
			continue
		}
		seg := strings.SplitN(rel, "/", 2)[0]
		if _, ok := seen[seg]; ok {
			continue
		}
		seen[seg] = struct{}{}
		out = append(out, seg)
	}
	sort.Strings(out)
	return out
}

func looksLikeManga(prefix string, entries []zipEntry) bool {
	if hasCoverOrInfo(prefix, entries) {
		return true
	}
	return hasChapterDirs(prefix, entries)
}

func hasCoverOrInfo(prefix string, entries []zipEntry) bool {
	for _, e := range entries {
		rel := trimPrefix(e.Path, prefix)
		if rel == "" || strings.Contains(rel, "/") {
			continue
		}
		low := strings.ToLower(rel)
		if low == "info.json" || isCoverName(low) {
			return true
		}
	}
	return false
}

func hasDirectImages(prefix string, entries []zipEntry) bool {
	for _, e := range entries {
		rel := trimPrefix(e.Path, prefix)
		if rel == "" || strings.Contains(rel, "/") {
			continue
		}
		if imageExt[strings.ToLower(path.Ext(rel))] {
			return true
		}
	}
	return false
}

func hasChapterDirs(prefix string, entries []zipEntry) bool {
	return len(collectChapterDirs(prefix, entries)) > 0
}

func collectChapterDirs(prefix string, entries []zipEntry) []string {
	joinAll := func(names []string, parent string) []string {
		out := make([]string, 0, len(names))
		for _, d := range names {
			out = append(out, joinPrefix(parent, d))
		}
		return out
	}

	// 规范目录：漫画根下「漫画名第N话」
	var hua, other []string
	for _, d := range subdirsWithImages(prefix, entries) {
		if strings.EqualFold(d, "chapters") {
			continue
		}
		if reHua.MatchString(d) {
			hua = append(hua, d)
		} else {
			other = append(other, d)
		}
	}
	if len(hua) > 0 {
		return joinAll(hua, prefix)
	}
	fromChapters := subdirsWithImages(joinPrefix(prefix, "chapters"), entries)
	if len(fromChapters) > 0 {
		return joinAll(fromChapters, joinPrefix(prefix, "chapters"))
	}
	return joinAll(other, prefix)
}

func subdirsWithImages(prefix string, entries []zipEntry) []string {
	hasImg := map[string]bool{}
	for _, e := range entries {
		rel := trimPrefix(e.Path, prefix)
		if rel == "" || !strings.Contains(rel, "/") {
			continue
		}
		seg := strings.SplitN(rel, "/", 2)[0]
		rest := strings.SplitN(rel, "/", 2)[1]
		if strings.Contains(rest, "/") {
			continue
		}
		if imageExt[strings.ToLower(path.Ext(rest))] {
			hasImg[seg] = true
		}
	}
	out := make([]string, 0, len(hasImg))
	for d := range hasImg {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		si, sj := chapterSeq(out[i]), chapterSeq(out[j])
		if si != sj {
			return si < sj
		}
		return out[i] < out[j]
	})
	return out
}

func parseOneManga(prefix string, entries []zipEntry) (parsedManga, error) {
	m := parsedManga{Root: prefix, Title: displayName(prefix)}
	var infoFile *zip.File
	for _, e := range entries {
		rel := trimPrefix(e.Path, prefix)
		if rel == "" || strings.Contains(rel, "/") {
			continue
		}
		low := strings.ToLower(rel)
		if low == "info.json" {
			infoFile = e.File
			continue
		}
		if isCoverName(low) {
			m.Cover = e.File
			m.CoverExt = strings.ToLower(path.Ext(low))
		}
	}
	if infoFile != nil {
		applyInfoJSON(infoFile, &m)
	}

	dirs := collectChapterDirs(prefix, entries)
	if len(dirs) == 0 {
		return m, errZip("「" + m.Title + "」没有找到章节页图")
	}
	if len(dirs) > 300 {
		return m, errZip("「" + m.Title + "」章节过多")
	}
	used := map[int]struct{}{}
	for i, dir := range dirs {
		ch := parsedChapter{
			Dir:   dir,
			Title: displayName(path.Base(dir)),
			Seq:   chapterSeq(path.Base(dir)),
		}
		infoTitle := ""
		if f := fileInDir(dir, "chapter_info.json", entries); f != nil {
			t, n := readChapterInfo(f)
			infoTitle = t
			if ch.Seq <= 0 && n > 0 {
				ch.Seq = n
			}
		}
		if ch.Seq <= 0 {
			ch.Seq = i + 1
		}
		for {
			if _, ok := used[ch.Seq]; !ok {
				break
			}
			ch.Seq++
		}
		used[ch.Seq] = struct{}{}
		ch.Pages = pagesInDir(dir, entries)
		if len(ch.Pages) == 0 {
			continue
		}
		if len(ch.Pages) > 500 {
			return m, errZip("「" + m.Title + "」单章页数超过 500")
		}
		ch.Title = finalizeChapterTitle(ch.Title, m.Title, infoTitle, ch.Seq)
		m.Chapters = append(m.Chapters, ch)
	}
	if len(m.Chapters) == 0 {
		return m, errZip("「" + m.Title + "」章节里没有图片")
	}
	sort.Slice(m.Chapters, func(i, j int) bool { return m.Chapters[i].Seq < m.Chapters[j].Seq })
	return m, nil
}

func pagesInDir(dir string, entries []zipEntry) []parsedPage {
	var pages, aux []parsedPage
	for _, e := range entries {
		rel := trimPrefix(e.Path, dir)
		if rel == "" || strings.Contains(rel, "/") {
			continue
		}
		ext := strings.ToLower(path.Ext(rel))
		if !imageExt[ext] {
			continue
		}
		item := parsedPage{Name: rel, File: e.File}
		if isAuxChapterImage(rel) {
			aux = append(aux, item)
			continue
		}
		pages = append(pages, item)
	}
	if len(pages) == 0 {
		pages = aux
	}
	sort.Slice(pages, func(i, j int) bool {
		si, sj := pageSeq(pages[i].Name), pageSeq(pages[j].Name)
		if si != sj {
			return si < sj
		}
		return strings.ToLower(pages[i].Name) < strings.ToLower(pages[j].Name)
	})
	return pages
}

func fileInDir(dir, name string, entries []zipEntry) *zip.File {
	want := strings.ToLower(name)
	for _, e := range entries {
		rel := trimPrefix(e.Path, dir)
		if strings.ToLower(rel) == want {
			return e.File
		}
	}
	return nil
}

func applyInfoJSON(f *zip.File, m *parsedManga) {
	raw := readJSONMap(f)
	if raw == nil {
		return
	}
	if title := jsonMapString(raw, "title", "name", "comic_name"); title != "" {
		m.Title = title
	}
	if s := jsonMapString(raw, "intro", "description", "desc", "summary"); s != "" {
		m.Intro = s
	}
	if s := jsonMapString(raw, "author", "writer"); s != "" {
		m.Author = s
	}
	if s := jsonMapString(raw, "category", "cate", "tag"); s != "" {
		m.Category = s
	} else if s := jsonMapStringSlice(raw, "types", "tags", "categories"); s != "" {
		m.Category = s
	}
}

func readChapterInfo(f *zip.File) (title string, num int) {
	raw := readJSONMap(f)
	if raw == nil {
		return "", 0
	}
	title = jsonMapString(raw, "title", "name")
	num = jsonMapInt(raw, "num", "index", "seq", "ep")
	return title, num
}

func readJSONMap(f *zip.File) map[string]any {
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	return raw
}

func jsonMapString(raw map[string]any, keys ...string) string {
	if raw == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := raw[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func jsonMapStringSlice(raw map[string]any, keys ...string) string {
	if raw == nil {
		return ""
	}
	for _, k := range keys {
		v, ok := raw[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		case []any:
			parts := make([]string, 0, len(t))
			for _, item := range t {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					parts = append(parts, strings.TrimSpace(s))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, ",")
			}
		}
	}
	return ""
}

func jsonMapInt(raw map[string]any, keys ...string) int {
	for _, k := range keys {
		v, ok := raw[k]
		if !ok || v == nil {
			continue
		}
		switch n := v.(type) {
		case float64:
			if n > 0 {
				return int(n)
			}
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && i > 0 {
				return i
			}
		}
	}
	return 0
}

// Toomics 等源站的 chapter_info.title 经常是整部漫画名，不是「第N话」。
// 文件夹名带第N话时以文件夹为准；否则才用 info 里真正的话名。
func finalizeChapterTitle(folderTitle, mangaTitle, infoTitle string, seq int) string {
	folderTitle = strings.TrimSpace(folderTitle)
	mangaTitle = strings.TrimSpace(mangaTitle)
	infoTitle = strings.TrimSpace(infoTitle)
	if reHua.MatchString(folderTitle) {
		return folderTitle
	}
	if infoTitle != "" && (reHua.MatchString(infoTitle) || !sameChapterName(infoTitle, mangaTitle)) {
		return infoTitle
	}
	if seq > 0 && (folderTitle == "" || sameChapterName(folderTitle, mangaTitle)) {
		base := mangaTitle
		if base == "" {
			base = folderTitle
		}
		if base == "" {
			return "第" + strconv.Itoa(seq) + "话"
		}
		return base + " 第" + strconv.Itoa(seq) + "话"
	}
	if folderTitle != "" {
		return folderTitle
	}
	return infoTitle
}

func sameChapterName(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func isCoverName(filename string) bool {
	ext := strings.ToLower(path.Ext(filename))
	if !imageExt[ext] {
		return false
	}
	stem := strings.ToLower(strings.TrimSuffix(filename, ext))
	return coverNames[stem]
}

func isAuxChapterImage(filename string) bool {
	if isCoverName(filename) {
		return true
	}
	stem := strings.ToLower(strings.TrimSuffix(filename, path.Ext(filename)))
	return stem == "small-cover" || stem == "small_cover"
}

func pageSeq(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	if m := rePageNum.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return chapterSeq(name)
}

func chapterSeq(name string) int {
	base := strings.TrimSuffix(name, path.Ext(name))
	if m := reHua.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := reLeadingNum.FindStringSubmatch(base); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

func displayName(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return "未命名漫画"
	}
	base := path.Base(p)
	if m := reLeadingNum.FindStringSubmatch(base); len(m) == 2 {
		rest := strings.TrimLeft(base[len(m[1]):], " _-.")
		if rest != "" {
			return rest
		}
	}
	return base
}

func trimPrefix(p, prefix string) string {
	p = strings.TrimLeft(p, "/")
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return p
	}
	if p == prefix {
		return ""
	}
	if strings.HasPrefix(p, prefix+"/") {
		return p[len(prefix)+1:]
	}
	return ""
}

func joinPrefix(a, b string) string {
	a = strings.Trim(a, "/")
	b = strings.Trim(b, "/")
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "/" + b
}
