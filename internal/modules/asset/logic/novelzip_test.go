package logic

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseNovelsZip_CatchNovelLayout(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("短篇情色小说/情欲性爱/我与女神/meta.json", `{"title":"我与女神的美好情爱","status":"done"}`)
	add("短篇情色小说/情欲性爱/我与女神/01-我与女神的美好情爱.txt", "我与女神的美好情爱\n来源: https://example.com/a\n\n正文第一段。\n")
	add("长篇/热门/某长篇/01-序章.txt", "序章\n来源: https://example.com/b\n\n序章正文。\n")
	add("长篇/热门/某长篇/02-第二章.txt", "第二章\n来源: https://example.com/c\n\n第二章正文。\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	books, err := parseNovelsZip(zr)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("books=%d", len(books))
	}
	var short, long *parsedNovel
	for i := range books {
		if books[i].Title == "我与女神的美好情爱" {
			short = &books[i]
		}
		if books[i].Title == "某长篇" {
			long = &books[i]
		}
	}
	if short == nil || len(short.Chapters) != 1 || short.Chapters[0].Content != "正文第一段。" {
		t.Fatalf("short=%+v", short)
	}
	if short.Category != "情欲性爱" {
		t.Fatalf("short category=%s", short.Category)
	}
	if long == nil || len(long.Chapters) != 2 || long.Chapters[1].Title != "第二章" {
		t.Fatalf("long=%+v", long)
	}
}

func TestNovelWordCount(t *testing.T) {
	if n := novelWordCount("你好世界"); n != 4 {
		t.Fatalf("wordCount=%d", n)
	}
}
