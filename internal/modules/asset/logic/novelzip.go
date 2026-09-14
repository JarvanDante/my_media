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
	"unicode/utf8"
)

var (
	reNovelSeq = regexp.MustCompile(`^(\d+)`)
	txtExt     = map[string]bool{".txt": true}
	skipNovelNames = map[string]bool{
		"checkpoint.json": true,
		"done.json":       true,
		"index.json":      true,
		"index.md":        true,
		"meta.json":       true,
		"last_report.json": true,
	}
)

type parsedNovelChapter struct {
	Seq     int
	Title   string
	Content string
}

type parsedNovel struct {
	Title    string
	Category string
	Intro    string
	Cover    *zip.File
	Chapters []parsedNovelChapter
}

func parseNovelsZip(zr *zip.Reader) ([]parsedNovel, error) {
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

	groups := map[string][]zipEntry{}
	for _, e := range entries {
		dir := path.Dir(e.Path)
		if dir == "." {
			dir = ""
		}
		groups[dir] = append(groups[dir], e)
	}

	var books []parsedNovel
	for dir, files := range groups {
		if !dirHasNovelTxt(files) {
			continue
		}
		book, err := parseOneNovel(dir, files)
		if err != nil {
			return nil, err
		}
		if book != nil && len(book.Chapters) > 0 {
			books = append(books, *book)
		}
	}
	if len(books) == 0 {
		return nil, errZip("压缩包里没有小说正文（需要 .txt）")
	}
	sort.SliceStable(books, func(i, j int) bool {
		return naturalLess(books[i].Title, books[j].Title)
	})
	return books, nil
}

func dirHasNovelTxt(files []zipEntry) bool {
	for _, e := range files {
		base := strings.ToLower(path.Base(e.Path))
		if skipNovelNames[base] {
			continue
		}
		if txtExt[strings.ToLower(path.Ext(base))] {
			return true
		}
	}
	return false
}

func parseOneNovel(dir string, files []zipEntry) (*parsedNovel, error) {
	book := &parsedNovel{
		Title:    bookTitleFromDir(dir),
		Category: bookCategoryFromDir(dir),
	}
	var chapters []parsedNovelChapter
	for _, e := range files {
		base := path.Base(e.Path)
		low := strings.ToLower(base)
		ext := strings.ToLower(path.Ext(low))
		stem := strings.TrimSuffix(low, ext)
		switch {
		case low == "meta.json":
			applyNovelMeta(book, e.File)
		case coverNames[stem] && imageExt[ext]:
			book.Cover = e.File
		case skipNovelNames[low]:
			continue
		case txtExt[ext]:
			ch, err := readNovelTxt(e)
			if err != nil {
				return nil, err
			}
			if ch.Content == "" {
				continue
			}
			chapters = append(chapters, ch)
		}
	}
	if book.Title == "" {
		book.Title = "未命名小说"
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		if chapters[i].Seq != chapters[j].Seq {
			return chapters[i].Seq < chapters[j].Seq
		}
		return naturalLess(chapters[i].Title, chapters[j].Title)
	})
	used := map[int]struct{}{}
	next := 1
	for i := range chapters {
		if chapters[i].Seq <= 0 {
			for {
				if _, ok := used[next]; !ok {
					chapters[i].Seq = next
					break
				}
				next++
			}
		}
		used[chapters[i].Seq] = struct{}{}
		if chapters[i].Title == "" {
			chapters[i].Title = "第" + strconv.Itoa(chapters[i].Seq) + "章"
		}
	}
	book.Chapters = chapters
	return book, nil
}

func bookTitleFromDir(dir string) string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return ""
	}
	return path.Base(dir)
}

func bookCategoryFromDir(dir string) string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return ""
	}
	parent := path.Dir(dir)
	if parent == "." || parent == "" {
		return ""
	}
	name := path.Base(parent)
	if strings.EqualFold(name, "library") {
		return ""
	}
	return name
}

func applyNovelMeta(book *parsedNovel, f *zip.File) {
	rc, err := f.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return
	}
	var meta map[string]any
	if json.Unmarshal(raw, &meta) != nil {
		return
	}
	if t, _ := meta["title"].(string); strings.TrimSpace(t) != "" {
		book.Title = strings.TrimSpace(t)
	}
	if intro, _ := meta["intro"].(string); strings.TrimSpace(intro) != "" {
		book.Intro = strings.TrimSpace(intro)
	}
	if intro, _ := meta["description"].(string); book.Intro == "" && strings.TrimSpace(intro) != "" {
		book.Intro = strings.TrimSpace(intro)
	}
}

func readNovelTxt(e zipEntry) (parsedNovelChapter, error) {
	rc, err := e.File.Open()
	if err != nil {
		return parsedNovelChapter{}, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return parsedNovelChapter{}, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	text = strings.TrimSpace(text)
	base := strings.TrimSuffix(path.Base(e.Path), path.Ext(e.Path))
	seq, nameTitle := splitNovelFileName(base)

	title := ""
	body := text
	if text != "" {
		lines := strings.Split(text, "\n")
		first := strings.TrimSpace(lines[0])
		rest := lines[1:]
		for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
			rest = rest[1:]
		}
		if len(rest) > 0 && strings.HasPrefix(strings.TrimSpace(rest[0]), "来源:") {
			rest = rest[1:]
			for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
				rest = rest[1:]
			}
		}
		if first != "" {
			title = first
		}
		body = strings.TrimSpace(strings.Join(rest, "\n"))
		if body == "" {
			body = text
		}
	}
	if nameTitle != "" && (title == "" || title == nameTitle) {
		title = nameTitle
	}
	if title == "" {
		title = nameTitle
	}
	return parsedNovelChapter{Seq: seq, Title: title, Content: body}, nil
}

func splitNovelFileName(base string) (seq int, title string) {
	base = strings.TrimSpace(base)
	m := reNovelSeq.FindStringSubmatch(base)
	if len(m) == 2 {
		seq, _ = strconv.Atoi(m[1])
		rest := strings.TrimLeft(base[len(m[1]):], "-_. 　")
		return seq, strings.TrimSpace(rest)
	}
	return 0, base
}

func novelWordCount(s string) int {
	return utf8.RuneCountInString(s)
}
