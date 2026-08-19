package logic

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseComicsZip_ChaptersLayout(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pack.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("6447/cover.jpg", "cover")
	add("6447/info.json", `{"title":"测试漫画","intro":"简介","category":"韩漫"}`)
	add("6447/chapters/001_游戏规则/page_001.jpg", "p1")
	add("6447/chapters/001_游戏规则/page_002.jpg", "p2")
	add("6447/chapters/002_下一话/page_001.jpg", "p3")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	list, err := parseComicsZip(&zr.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("manga count=%d", len(list))
	}
	m := list[0]
	if m.Title != "测试漫画" {
		t.Fatalf("title=%s", m.Title)
	}
	if m.Category != "韩漫" || m.Intro != "简介" {
		t.Fatalf("meta %+v", m)
	}
	if m.Cover == nil {
		t.Fatal("missing cover")
	}
	if len(m.Chapters) != 2 {
		t.Fatalf("chapters=%d", len(m.Chapters))
	}
	if m.Chapters[0].Seq != 1 || len(m.Chapters[0].Pages) != 2 {
		t.Fatalf("ch1 %+v", m.Chapters[0])
	}
	if m.Chapters[1].Seq != 2 || len(m.Chapters[1].Pages) != 1 {
		t.Fatalf("ch2 %+v", m.Chapters[1])
	}
}

func TestParseComicsZip_DirectChapterFolders(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "pack.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	add := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("游戏规则/cover.jpg", "cover")
	add("游戏规则/001_第一话/01.jpg", "p1")
	add("游戏规则/002_第二话/01.jpg", "p2")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	list, err := parseComicsZip(&zr.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "游戏规则" {
		t.Fatalf("got %+v", list)
	}
	if len(list[0].Chapters) != 2 {
		t.Fatalf("chapters=%d", len(list[0].Chapters))
	}
}
