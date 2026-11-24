package logic

import (
	"encoding/json"
	"os"
	"strings"
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

func TestSanitizeZipEntryNamePreservesSpacesAndRejectsFold(t *testing.T) {
	keep := []string{
		"剧名/01.mp4",
		" 剧名/01.mp4",
		"剧名 /01.mp4",
		"剧名/01.mp4 ",
		"\u00a0剧名/01.mp4",
		"剧名\u00a0/01.mp4",
		"\u3000剧名/01.mp4",
		"pack/ 剧名/01.mp4",
		"剧名/ 01.mp4",
		`dir\01.mp4`,
	}
	for _, name := range keep {
		got, err := sanitizeZipEntryName(name)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		want := strings.ReplaceAll(name, `\`, "/")
		if got != want {
			t.Fatalf("sanitize %q got %q want %q", name, got, want)
		}
	}
	for _, name := range []string{"a/./b/01.mp4", "a//b/01.mp4", "foo/../bar/01.mp4", "./x.mp4", "a/b/"} {
		if _, err := sanitizeZipEntryName(name); err == nil {
			t.Fatalf("should reject ambiguous path %q", name)
		}
	}
	if _, err := sanitizeZipEntryName("../etc/passwd"); err == nil {
		t.Fatal("zip slip")
	}
	if _, err := sanitizeZipEntryName("/tmp/x.mp4"); err == nil {
		t.Fatal("absolute")
	}
	if _, err := sanitizeZipEntryName("C:foo/a.mp4"); err == nil {
		t.Fatal("drive")
	}
}

func TestGroupAndPlanPreserveDirectoryIdentity(t *testing.T) {
	nbsp := "\u00a0剧名/01.mp4"
	ideo := "\u3000剧名/01.mp4"
	files := []zipMeta{
		{Name: "剧名/01.mp4", UncompressedSize: 1},
		{Name: " 剧名/01.mp4", UncompressedSize: 1},
		{Name: "剧名 /01.mp4", UncompressedSize: 1},
		{Name: nbsp, UncompressedSize: 1},
		{Name: ideo, UncompressedSize: 1},
		{Name: "剧名/cover.jpg", UncompressedSize: 1},
		{Name: " 剧名/cover.jpg", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 5 {
		t.Fatalf("want 5 groups, got %d %+v", len(groups), groups)
	}
	keys := map[string]bool{}
	for _, g := range groups {
		if g.ItemKey == "" || keys[g.ItemKey] {
			t.Fatalf("item key collision or empty: %+v", groups)
		}
		keys[g.ItemKey] = true
		if strings.TrimSpace(g.Title) != "剧名" {
			t.Fatalf("display title should trim, got %q", g.Title)
		}
	}
	if !keys["剧名"] || !keys[" 剧名"] || !keys["剧名 "] || !keys["\u00a0剧名"] || !keys["\u3000剧名"] {
		t.Fatalf("keys %v", keys)
	}
	mustPath := func(p string) {
		found := false
		for _, g := range groups {
			for _, v := range g.Videos {
				if v.Path == p {
					found = true
				}
			}
			if g.CoverPath == p {
				found = true
			}
			for _, c := range g.Covers {
				if c.Path == p {
					found = true
				}
			}
		}
		if !found {
			t.Fatalf("path %q not preserved in groups", p)
		}
	}
	mustPath("剧名/01.mp4")
	mustPath(" 剧名/01.mp4")
	mustPath("剧名 /01.mp4")
	mustPath(nbsp)
	mustPath(ideo)
	mustPath(" 剧名/cover.jpg")

	plan := DramaImportPlan{Items: []DramaImportPlanItem{
		{Title: "剧名", Cover: "剧名/cover.jpg", Episodes: []DramaImportPlanEp{{Path: "剧名/01.mp4", Seq: 1}}},
		{Title: " 剧名", Cover: " 剧名/cover.jpg", Episodes: []DramaImportPlanEp{{Path: " 剧名/01.mp4", Seq: 1}}},
		{Title: "剧名 ", Episodes: []DramaImportPlanEp{{Path: "剧名 /01.mp4", Seq: 1}}},
		{Title: "nbsp", Episodes: []DramaImportPlanEp{{Path: nbsp, Seq: 1}}},
		{Title: "ideo", Episodes: []DramaImportPlanEp{{Path: ideo, Seq: 1}}},
	}}
	raw, _ := json.Marshal(plan)
	out, err := applyDramaPlan(groups, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureUniqueDramaItemKeys(out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 5 {
		t.Fatalf("plan should keep 5 groups, got %d", len(out))
	}
	gotKeys := map[string]string{}
	for _, g := range out {
		gotKeys[g.ItemKey] = g.Videos[0].Path
		if g.ItemKey == " 剧名" {
			if g.CoverPath != " 剧名/cover.jpg" || g.Videos[0].Path != " 剧名/01.mp4" {
				t.Fatalf("leading space paths trimmed: %+v", g)
			}
		}
	}
	if gotKeys["剧名"] != "剧名/01.mp4" || gotKeys[" 剧名"] != " 剧名/01.mp4" || gotKeys["剧名 "] != "剧名 /01.mp4" {
		t.Fatalf("plan paths %v", gotKeys)
	}
}

func TestGroupFilenameSpacesStayInSameSeries(t *testing.T) {
	files := []zipMeta{
		{Name: "剧名/01.mp4", UncompressedSize: 1},
		{Name: "剧名/01.mp4 ", UncompressedSize: 1},
		{Name: "剧名/ 02.mp4", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Videos) != 3 {
		t.Fatalf("want 1 group 3 videos, got %+v", groups)
	}
	seen := map[string]bool{}
	for _, v := range groups[0].Videos {
		seen[v.Path] = true
	}
	for _, p := range []string{"剧名/01.mp4", "剧名/01.mp4 ", "剧名/ 02.mp4"} {
		if !seen[p] {
			t.Fatalf("missing path %q in %+v", p, seen)
		}
	}
	plan, _ := json.Marshal(DramaImportPlan{Items: []DramaImportPlanItem{{
		Title: "剧名",
		Episodes: []DramaImportPlanEp{
			{Path: "剧名/01.mp4", Seq: 1},
			{Path: "剧名/01.mp4 ", Seq: 2},
			{Path: "剧名/ 02.mp4", Seq: 3},
		},
	}}})
	out, err := applyDramaPlan(groups, string(plan))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Videos) != 3 {
		t.Fatalf("%+v", out)
	}
}

func TestGroupBackslashRootNestedAndSameNameDirs(t *testing.T) {
	files := []zipMeta{
		{Name: `剧A\01.mp4`, UncompressedSize: 1},
		{Name: `剧B\01.mp4`, UncompressedSize: 1},
		{Name: "01.mp4", UncompressedSize: 1},
		{Name: " 01.mp4", UncompressedSize: 1},
		{Name: "pack/剧名/01.mp4", UncompressedSize: 1},
		{Name: "other/剧名/01.mp4", UncompressedSize: 1},
		{Name: "wrap/inner/剧C/01.mp4", UncompressedSize: 1},
	}
	groups, err := GroupDramaZip(files, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]DramaZipGroup{}
	for _, g := range groups {
		byKey[g.ItemKey] = g
	}
	if _, ok := byKey["剧A"]; !ok {
		t.Fatalf("backslash dir 剧A missing: %v", keysOf(byKey))
	}
	if byKey["剧A"].Videos[0].Path != "剧A/01.mp4" {
		t.Fatalf("backslash not unified: %q", byKey["剧A"].Videos[0].Path)
	}
	if _, ok := byKey["剧B"]; !ok {
		t.Fatalf("剧B missing")
	}
	root, ok := byKey["."]
	if !ok || len(root.Videos) != 2 {
		t.Fatalf("root 01.mp4 and leading-space file should share ItemKey ., got %+v", root)
	}
	seen := map[string]bool{}
	for _, v := range root.Videos {
		seen[v.Path] = true
	}
	if !seen["01.mp4"] || !seen[" 01.mp4"] {
		t.Fatalf("root paths %v", seen)
	}
	// 无公共父目录时，身份是第一段目录，同名叶子仍因父段不同而分开。
	if _, ok := byKey["pack"]; !ok {
		t.Fatalf("pack missing: %v", keysOf(byKey))
	}
	if byKey["pack"].Videos[0].Path != "pack/剧名/01.mp4" {
		t.Fatalf("pack path %q", byKey["pack"].Videos[0].Path)
	}
	if _, ok := byKey["other"]; !ok {
		t.Fatalf("other missing: %v", keysOf(byKey))
	}
	if byKey["other"].Videos[0].Path != "other/剧名/01.mp4" {
		t.Fatalf("other path %q", byKey["other"].Videos[0].Path)
	}
	if _, ok := byKey["wrap"]; !ok {
		t.Fatalf("nested wrap missing: %v", keysOf(byKey))
	}
	if byKey["wrap"].Videos[0].Path != "wrap/inner/剧C/01.mp4" {
		t.Fatalf("nested path %q", byKey["wrap"].Videos[0].Path)
	}

	// 公共父目录下，同层不同目录是两组。
	sibling, err := GroupDramaZip([]zipMeta{
		{Name: "pack/剧名/01.mp4", UncompressedSize: 1},
		{Name: "pack/别的/01.mp4", UncompressedSize: 1},
	}, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	sib := map[string]bool{}
	for _, g := range sibling {
		sib[g.ItemKey] = true
	}
	if !sib["pack/剧名"] || !sib["pack/别的"] || len(sibling) != 2 {
		t.Fatalf("common parent siblings %v", sib)
	}
}

func keysOf(m map[string]DramaZipGroup) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestGroupRejectsAmbiguousDotPathBeforeBucket(t *testing.T) {
	_, err := GroupDramaZip([]zipMeta{
		{Name: "剧名/01.mp4", UncompressedSize: 1},
		{Name: "剧名/./01.mp4", UncompressedSize: 1},
	}, "pack.zip")
	if err == nil {
		t.Fatal("dot path should be rejected before grouping")
	}
	if !strings.Contains(err.Error(), "拒绝") && !strings.Contains(err.Error(), "点路径") {
		t.Fatalf("want clear reject, got %v", err)
	}
}

func TestApplyPlanRejectsAmbiguousAndMissingPaths(t *testing.T) {
	groups, err := GroupDramaZip([]zipMeta{{Name: "剧名/01.mp4", UncompressedSize: 1}}, "pack.zip")
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := json.Marshal(DramaImportPlan{Items: []DramaImportPlanItem{{
		Title:    "剧名",
		Episodes: []DramaImportPlanEp{{Path: "剧名/./01.mp4", Seq: 1}},
	}}})
	if _, err := applyDramaPlan(groups, string(plan)); err == nil {
		t.Fatal("plan dot path should fail")
	}
}
