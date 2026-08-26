package playsign

import "testing"

func TestComicPageRel(t *testing.T) {
	got := comicPageRel("ABC", "comics/ABC/ch001/page_001.bnc")
	if got != "ch001/page_001.bnc" {
		t.Fatalf("got %q", got)
	}
	if comicPageRel("ABC", "comics/ABC/ch001/a/b.jpg") != "" {
		t.Fatal("deep path should reject")
	}
}

func TestWithImageExt(t *testing.T) {
	if withImageExt("ch001/page_001.jpg", ".bnc") != "ch001/page_001.bnc" {
		t.Fatal(withImageExt("ch001/page_001.jpg", ".bnc"))
	}
	if withImageExt("ch001/page_001.bnc", ".jpg") != "ch001/page_001.jpg" {
		t.Fatal(withImageExt("ch001/page_001.bnc", ".jpg"))
	}
}
