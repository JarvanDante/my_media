package logic

import (
	"os"
	"testing"
)

func TestParseEpisodeSeq(t *testing.T) {
	cases := map[string]int{
		"少女露出爱好者_第01集.mp4": 1,
		"母女日常_第10集.mp4":    10,
		"母女日常_第2集.mp4":     2,
		"第1集.mp4":          1,
		"01.mp4":           1,
		"1.mp4":            1,
		"EP02.mp4":         2,
		"剧名_第03话.mkv":      3,
	}
	for name, want := range cases {
		got, ok := ParseEpisodeSeq(name)
		if !ok || got != want {
			t.Fatalf("%s: got %d ok=%v want %d", name, got, ok, want)
		}
	}
	if _, ok := ParseEpisodeSeq("封面.jpeg"); ok {
		t.Fatal("image should not parse as episode")
	}
	// 目录名里的「全5集」不应作为文件集号来源：无第N集时不能误取 5
	if seq, ok := ParseEpisodeSeq("患性瘾的巨乳女教师（全5集）.mp4"); ok && seq == 5 {
		t.Fatalf("should not take 全5集 as episode, got %d", seq)
	}
}

func TestParseEpisodeNaturalOrder(t *testing.T) {
	names := []string{"第10集.mp4", "第2集.mp4", "第1集.mp4"}
	// grouping sorts by numeric seq, not lexical 1,10,2
	files := []zipMeta{
		{Name: "母女日常/母女日常_第10集.mp4", UncompressedSize: 10},
		{Name: "母女日常/母女日常_第02集.mp4", UncompressedSize: 10},
		{Name: "母女日常/母女日常_第01集.mp4", UncompressedSize: 10},
		{Name: "母女日常/abc.jpeg", UncompressedSize: 20},
	}
	groups, err := GroupDramaZip(files, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Title != "母女日常" {
		t.Fatalf("groups=%+v", groups)
	}
	if groups[0].CoverPath != "母女日常/abc.jpeg" {
		t.Fatalf("cover %s", groups[0].CoverPath)
	}
	got := []int{}
	for _, v := range groups[0].Videos {
		got = append(got, v.Seq)
	}
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 10 {
		t.Fatalf("seq order %v (from %v)", got, names)
	}
}

func TestGroupKeepsParenthesesTitleAndPackFolder(t *testing.T) {
	files := []zipMeta{
		{Name: "tv/丈夫的复仇（全1集）/丈夫的复仇（全1集）_第01集.mp4", UncompressedSize: 100},
		{Name: "tv/丈夫的复仇（全1集）/e65e4a8c-6a64-489d-a6c5-7e.jpeg", UncompressedSize: 20},
		{Name: "tv/【陆雪琪堕落】/【陆雪琪堕落】_第01集.mp4", UncompressedSize: 100},
		{Name: "tv/.DS_Store", UncompressedSize: 8},
		{Name: "tv/__MACOSX/foo.txt", UncompressedSize: 8},
	}
	groups, err := GroupDramaZip(files, "tv.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %d %+v", len(groups), groups)
	}
	titles := map[string]bool{}
	for _, g := range groups {
		titles[g.Title] = true
		if g.Title != "丈夫的复仇（全1集）" && g.Title != "【陆雪琪堕落】" {
			t.Fatalf("stripped or unexpected title %q", g.Title)
		}
	}
	if !titles["丈夫的复仇（全1集）"] || !titles["【陆雪琪堕落】"] {
		t.Fatalf("titles %v", titles)
	}
}

func TestGroupSingleDramaDir(t *testing.T) {
	files := []zipMeta{
		{Name: "秘密教学/秘密教学_第01集.mp4", UncompressedSize: 1},
		{Name: "秘密教学/秘密教学_第07集.mp4", UncompressedSize: 1},
		{Name: "秘密教学/cover.jpg", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "秘密教学.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Title != "秘密教学" || len(groups[0].Videos) != 2 {
		t.Fatalf("%+v", groups)
	}
	if groups[0].CoverPath != "秘密教学/cover.jpg" {
		t.Fatalf("cover %s", groups[0].CoverPath)
	}
}

func TestDuplicateSeqAndMultiCoverNeedConfirm(t *testing.T) {
	files := []zipMeta{
		{Name: "剧A/第01集.mp4", UncompressedSize: 1},
		{Name: "剧A/another_第01集.mp4", UncompressedSize: 1},
		{Name: "剧A/a.jpg", UncompressedSize: 1},
		{Name: "剧A/b.jpg", UncompressedSize: 1},
		{Name: "剧A/readme.txt", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "x.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || !groups[0].NeedsConfirm {
		t.Fatalf("should need confirm: %+v", groups[0])
	}
	if groups[0].CoverPath != "" {
		t.Fatalf("must not silently pick cover %s", groups[0].CoverPath)
	}
}

func TestMissingSeqNeedsConfirm(t *testing.T) {
	files := []zipMeta{
		{Name: "剧A/foo.mp4", UncompressedSize: 1},
		{Name: "剧A/cover.jpg", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "x.zip")
	if err != nil {
		t.Fatal(err)
	}
	if !groups[0].NeedsConfirm || groups[0].Videos[0].Has {
		t.Fatalf("%+v", groups[0])
	}
}

func TestZipSlipAndSymlinkRejected(t *testing.T) {
	_, err := GroupDramaZip([]zipMeta{{Name: "../etc/passwd", UncompressedSize: 1}}, "x.zip")
	if err == nil {
		t.Fatal("zip slip should fail")
	}
	_, err = GroupDramaZip([]zipMeta{{Name: "/tmp/x.mp4", UncompressedSize: 1}}, "x.zip")
	if err == nil {
		t.Fatal("absolute path should fail")
	}
	_, err = GroupDramaZip([]zipMeta{{Name: "a/e.mp4", UncompressedSize: 1, Mode: os.ModeSymlink}}, "x.zip")
	if err == nil {
		t.Fatal("symlink should fail")
	}
}

func TestCheckZipLimits(t *testing.T) {
	lim := DefaultDramaImportLimits()
	files := []zipMeta{{Name: "a/a.mp4", UncompressedSize: 100, CompressedSize: 100}}
	if err := checkZipLimits(100, files, lim); err != nil {
		t.Fatal(err)
	}
	if err := checkZipLimits(lim.MaxZipBytes+1, files, lim); err == nil {
		t.Fatal("zip too large")
	}
	if err := checkZipLimits(100, []zipMeta{{Name: "a/a.mp4", UncompressedSize: lim.MaxFileBytes + 1}}, lim); err == nil {
		t.Fatal("file too large")
	}
	if err := checkZipLimits(10, []zipMeta{{Name: "a/a.mp4", UncompressedSize: 10 * 1000}}, lim); err == nil {
		t.Fatal("ratio too high")
	}
}

func TestSanitizeZipEntryName(t *testing.T) {
	if _, err := sanitizeZipEntryName("ok/a.mp4"); err != nil {
		t.Fatal(err)
	}
	if _, err := sanitizeZipEntryName("foo/../../etc/passwd"); err == nil {
		t.Fatal("expected slip")
	}
}
