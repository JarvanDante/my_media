package logic

import (
	"archive/zip"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/JarvanDante/my_media/internal/dao"
)

// HTTP 入口（分片代传、兼容整包 POST）仍受公网 Cloudflare 约 100MB 单请求限制。
// 整包经分片落到 MinIO 后由异步任务处理，压缩包/单文件上限与请求体解耦，恢复大文件能力。
const (
	defaultMaxBodyBytes     int64   = 100_000_000
	defaultMultipartMargin  int64   = 4_000_000
	defaultMaxChunkBytes    int64   = 32 << 20
	defaultMaxZipBytes      int64   = 8 << 30
	defaultMaxExtractBytes  int64   = 16 << 30
	defaultMaxFiles         int     = 2000
	defaultMaxFileBytes     int64   = 2 << 30
	defaultMaxRatio         float64 = 100
	defaultMinFreeBytes     int64   = 1 << 30
	defaultImportTimeoutSec int     = 600
	defaultJobTimeoutSec    int     = 7200
	defaultMaxRunningJobs   int     = 1
)

type DramaImportLimits struct {
	MaxZipBytes      int64
	MaxBodyBytes     int64
	MaxChunkBytes    int64
	MaxExtractBytes  int64
	MaxFiles         int
	MaxFileBytes     int64
	MaxRatio         float64
	MinFreeBytes     int64
	ImportTimeoutSec int
	JobTimeoutSec    int
	MaxRunningJobs   int
}

func DefaultDramaImportLimits() DramaImportLimits {
	return DramaImportLimits{
		MaxZipBytes:      defaultMaxZipBytes,
		MaxBodyBytes:     defaultMaxBodyBytes,
		MaxChunkBytes:    defaultMaxChunkBytes,
		MaxExtractBytes:  defaultMaxExtractBytes,
		MaxFiles:         defaultMaxFiles,
		MaxFileBytes:     defaultMaxFileBytes,
		MaxRatio:         defaultMaxRatio,
		MinFreeBytes:     defaultMinFreeBytes,
		ImportTimeoutSec: defaultImportTimeoutSec,
		JobTimeoutSec:    defaultJobTimeoutSec,
		MaxRunningJobs:   defaultMaxRunningJobs,
	}.withDefaults()
}

func (l DramaImportLimits) withDefaults() DramaImportLimits {
	if l.MaxBodyBytes <= 0 {
		l.MaxBodyBytes = defaultMaxBodyBytes
	}
	if l.MaxChunkBytes <= 0 {
		l.MaxChunkBytes = defaultMaxChunkBytes
	}
	if l.MaxChunkBytes >= l.MaxBodyBytes {
		// 分片须显著小于公网单请求上限，预留协议余量
		capChunk := l.MaxBodyBytes - defaultMultipartMargin
		if capChunk > 32<<20 {
			capChunk = 32 << 20
		}
		if capChunk < 1 {
			capChunk = l.MaxBodyBytes
		}
		l.MaxChunkBytes = capChunk
	}
	if l.MaxZipBytes <= 0 {
		l.MaxZipBytes = defaultMaxZipBytes
	}
	if l.MaxExtractBytes <= 0 {
		l.MaxExtractBytes = defaultMaxExtractBytes
	}
	if l.MaxFiles <= 0 {
		l.MaxFiles = defaultMaxFiles
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = defaultMaxFileBytes
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = defaultMaxRatio
	}
	if l.MinFreeBytes <= 0 {
		l.MinFreeBytes = defaultMinFreeBytes
	}
	if l.ImportTimeoutSec <= 0 {
		l.ImportTimeoutSec = defaultImportTimeoutSec
	}
	if l.JobTimeoutSec <= 0 {
		l.JobTimeoutSec = defaultJobTimeoutSec
	}
	if l.MaxRunningJobs <= 0 {
		l.MaxRunningJobs = defaultMaxRunningJobs
	}
	if l.MaxChunkBytes > l.MaxZipBytes {
		l.MaxChunkBytes = l.MaxZipBytes
	}
	return l
}

func MaxDramaImportParts(lim DramaImportLimits) int {
	lim = lim.withDefaults()
	if lim.MaxChunkBytes <= 0 {
		return 1
	}
	n := int((lim.MaxZipBytes + lim.MaxChunkBytes - 1) / lim.MaxChunkBytes)
	if n < 1 {
		return 1
	}
	return n
}

var (
	videoExt = map[string]bool{
		".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".m4v": true, ".avi": true,
	}
	imageExtDrama = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	}
	reDramaHua = regexp.MustCompile(`第\s*0*(\d+)\s*[集话章回話]`)
	reDramaEp  = regexp.MustCompile(`(?i)(?:^|[_\-.\s])(?:ep|e|episode|sp)?\s*0*(\d+)(?:\s*[集话章回]|\s*$|\.)`)
)

type zipMeta struct {
	Name             string
	CompressedSize   int64
	UncompressedSize int64
	Mode             os.FileMode
}

type dramaFile struct {
	Path string
	Name string
	Size int64
	Seq  int
	Has  bool
}

type DramaZipGroup struct {
	Title        string
	TargetID     string
	ItemKey      string // 规范目录标识，同一任务内稳定；不用可编辑标题
	Videos       []dramaFile
	Covers       []dramaFile
	CoverPath    string
	Skipped      []string
	Warnings     []string
	NeedsConfirm bool
}

func shouldIgnoreZipName(name string) bool {
	name = strings.ReplaceAll(name, "\\", "/")
	// 只统一分隔符，不 TrimSpace：前导/尾部空白是路径身份的一部分。
	if name == "" || strings.HasSuffix(name, "/") {
		return true
	}
	base := path.Base(name)
	if base == ".DS_Store" || base == "Thumbs.db" || base == "desktop.ini" {
		return true
	}
	if strings.HasPrefix(base, "._") || strings.HasPrefix(base, "~$") {
		return true
	}
	if strings.HasSuffix(strings.ToLower(base), ".tmp") || strings.HasSuffix(strings.ToLower(base), ".temp") {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == "__MACOSX" || strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// dramaMediaExt 只为识别视频/封面类型去掉扩展名两侧空白；完整路径身份仍保留文件名空白。
func dramaMediaExt(name string) string {
	return strings.ToLower(strings.TrimSpace(path.Ext(name)))
}

func sanitizeZipEntryName(name string) (string, error) {
	if strings.Contains(name, "\x00") {
		return "", fmt.Errorf("非法路径")
	}
	// 与绑定 item key 同一无损规范：只统一分隔符，不 TrimSpace。
	n := strings.ReplaceAll(name, "\\", "/")
	if n == "" {
		return "", fmt.Errorf("空路径")
	}
	if strings.HasPrefix(n, "/") || filepath.IsAbs(n) || (len(n) > 1 && n[1] == ':') {
		return "", fmt.Errorf("拒绝绝对路径: %s", name)
	}
	clean := path.Clean(n)
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("拒绝越出目录的路径: %s", name)
	}
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" || clean == "/" {
		return "", fmt.Errorf("空路径")
	}
	// 点路径或多余分隔符折叠会改变身份，合桶前明确拒绝，避免两组被静默合并。
	if clean != n {
		return "", fmt.Errorf("路径 %q 含有会混淆的点路径或多余分隔符，拒绝规范化合并", name)
	}
	return n, nil
}

func ParseEpisodeSeq(filename string) (int, bool) {
	stem := strings.TrimSuffix(path.Base(filename), path.Ext(filename))
	if stem == "" {
		return 0, false
	}
	if m := reDramaHua.FindStringSubmatch(stem); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			return n, true
		}
	}
	// 避免把目录名里的「全5集」当集号：只解析文件名。
	nums := reDramaEp.FindAllStringSubmatch(stem, -1)
	if len(nums) == 0 {
		// 纯数字文件名 01 / 1
		allDigit := true
		for _, r := range stem {
			if !unicode.IsDigit(r) {
				allDigit = false
				break
			}
		}
		if allDigit {
			n, err := strconv.Atoi(strings.TrimLeft(stem, "0"))
			if err != nil {
				if stem == strings.Repeat("0", len(stem)) {
					return 0, false
				}
			} else if n > 0 {
				return n, true
			}
			if n, err := strconv.Atoi(stem); err == nil && n > 0 {
				return n, true
			}
		}
		return 0, false
	}
	last := nums[len(nums)-1]
	n, err := strconv.Atoi(last[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func checkZipLimits(zipSize int64, files []zipMeta, lim DramaImportLimits) error {
	lim = lim.withDefaults()
	if zipSize > lim.MaxZipBytes {
		return fmt.Errorf("压缩包大小 %s 超过上限 %s", humanBytes(zipSize), humanBytes(lim.MaxZipBytes))
	}
	var extract int64
	count := 0
	for _, f := range files {
		if shouldIgnoreZipName(f.Name) {
			continue
		}
		if f.Mode&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝符号链接: %s", f.Name)
		}
		if f.UncompressedSize > lim.MaxFileBytes {
			return fmt.Errorf("文件 %s 未压缩大小 %s 超过单文件上限 %s", f.Name, humanBytes(f.UncompressedSize), humanBytes(lim.MaxFileBytes))
		}
		extract += f.UncompressedSize
		count++
		if count > lim.MaxFiles {
			return fmt.Errorf("压缩包文件数超过上限 %d", lim.MaxFiles)
		}
	}
	if extract > lim.MaxExtractBytes {
		return fmt.Errorf("解压总大小 %s 超过上限 %s", humanBytes(extract), humanBytes(lim.MaxExtractBytes))
	}
	if zipSize > 0 && extract > 0 {
		ratio := float64(extract) / float64(zipSize)
		if ratio > lim.MaxRatio {
			return fmt.Errorf("压缩比 %.1f 超过上限 %.0f，已拒绝", ratio, lim.MaxRatio)
		}
	}
	return nil
}

func ensureDiskFree(dir string, need int64) error {
	if need <= 0 {
		return nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return fmt.Errorf("无法检查磁盘空间: %w", err)
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	if free < need {
		return fmt.Errorf("磁盘剩余 %s 不足，至少需要 %s", humanBytes(free), humanBytes(need))
	}
	return nil
}

func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/1024/1024)
	}
	return fmt.Sprintf("%.2fGB", float64(n)/1024/1024/1024)
}

func GroupDramaZip(files []zipMeta, zipBase string) ([]DramaZipGroup, error) {
	type item struct {
		Path string
		Size int64
		Ext  string
	}
	var media []item
	var unsafe error
	skippedOther := map[string][]string{}
	for _, f := range files {
		if f.Mode&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("拒绝符号链接: %s", f.Name)
		}
		if shouldIgnoreZipName(f.Name) {
			continue
		}
		clean, err := sanitizeZipEntryName(f.Name)
		if err != nil {
			unsafe = err
			break
		}
		ext := dramaMediaExt(clean)
		if !videoExt[ext] && !imageExtDrama[ext] {
			dir := path.Dir(clean)
			skippedOther[dir] = append(skippedOther[dir], path.Base(clean))
			continue
		}
		media = append(media, item{Path: clean, Size: f.UncompressedSize, Ext: ext})
	}
	if unsafe != nil {
		return nil, unsafe
	}
	if len(media) == 0 {
		return nil, fmt.Errorf("压缩包里没有可识别的视频或封面")
	}

	paths := make([]string, 0, len(media))
	for _, it := range media {
		paths = append(paths, it.Path)
	}
	parent := commonPackParent(paths)
	buckets := map[string][]item{}
	order := []string{}
	for _, it := range media {
		key, _ := dramaGroupKey(it.Path, parent, zipBase)
		if _, ok := buckets[key]; !ok {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], it)
	}

	var groups []DramaZipGroup
	seenItemKey := map[string]string{}
	for _, key := range order {
		items := buckets[key]
		title := groupTitle(key, zipBase)
		itemKey, err := dao.NormalizeDramaImportItemKey(key)
		if err != nil {
			return nil, err
		}
		if prev, ok := seenItemKey[itemKey]; ok {
			return nil, fmt.Errorf("压缩包目录 %q 与 %q 规范化后冲突，拒绝合并导入", prev, key)
		}
		seenItemKey[itemKey] = key
		g := DramaZipGroup{Title: title, ItemKey: itemKey}
		for _, it := range items {
			df := dramaFile{Path: it.Path, Name: path.Base(it.Path), Size: it.Size}
			if videoExt[it.Ext] {
				if seq, ok := ParseEpisodeSeq(it.Path); ok {
					df.Seq, df.Has = seq, true
				}
				g.Videos = append(g.Videos, df)
			} else {
				g.Covers = append(g.Covers, df)
			}
		}
		if extras, ok := skippedOther[key]; ok {
			for _, n := range extras {
				g.Skipped = append(g.Skipped, n+"（不支持的文件）")
			}
		}
		finalizeDramaGroup(&g)
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("没有可导入的短剧目录")
	}
	return groups, nil
}

func commonPackParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	first := strings.Split(paths[0], "/")
	if len(first) < 3 {
		return ""
	}
	root := first[0]
	for _, p := range paths {
		parts := strings.Split(p, "/")
		if len(parts) < 3 || parts[0] != root {
			return ""
		}
		if len(parts) == 2 {
			return ""
		}
	}
	// 根目录下没有直属媒体（都在 root/剧名/...）才视为父目录打包。
	for _, p := range paths {
		if path.Dir(p) == root {
			return ""
		}
	}
	return root
}

func dramaGroupKey(rel, parent, zipBase string) (key, title string) {
	parts := strings.Split(rel, "/")
	if parent != "" {
		if len(parts) >= 2 && parts[0] == parent {
			return parent + "/" + parts[1], parts[1]
		}
	}
	if len(parts) == 1 {
		t := strings.TrimSuffix(zipBase, path.Ext(zipBase))
		if t == "" {
			t = "未命名短剧"
		}
		return ".", t
	}
	return parts[0], parts[0]
}

func groupTitle(key, zipBase string) string {
	if key == "." {
		t := strings.TrimSuffix(zipBase, path.Ext(zipBase))
		if t == "" {
			return "未命名短剧"
		}
		return displayDramaTitle(t)
	}
	if i := strings.Index(key, "/"); i >= 0 {
		return displayDramaTitle(key[i+1:])
	}
	return displayDramaTitle(key)
}

// displayDramaTitle 仅用于展示标题，可去掉首尾空白；路径与 ItemKey 不得使用。
func displayDramaTitle(s string) string {
	if t := strings.TrimSpace(s); t != "" {
		return t
	}
	return s
}

func finalizeDramaGroup(g *DramaZipGroup) {
	sort.SliceStable(g.Videos, func(i, j int) bool {
		a, b := g.Videos[i], g.Videos[j]
		if a.Has && b.Has && a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		if a.Has != b.Has {
			return a.Has
		}
		return naturalLess(a.Name, b.Name)
	})
	seen := map[int]int{}
	missing := 0
	for _, v := range g.Videos {
		if !v.Has {
			missing++
			continue
		}
		seen[v.Seq]++
	}
	if len(g.Videos) == 0 {
		g.Warnings = append(g.Warnings, "没有视频文件")
		g.NeedsConfirm = true
	}
	if missing > 0 {
		g.Warnings = append(g.Warnings, fmt.Sprintf("%d 个文件无法识别集号，请确认序号", missing))
		g.NeedsConfirm = true
	}
	for seq, n := range seen {
		if n > 1 {
			g.Warnings = append(g.Warnings, fmt.Sprintf("集号 %d 重复 %d 次，请确认", seq, n))
			g.NeedsConfirm = true
		}
	}
	switch len(g.Covers) {
	case 0:
		g.Warnings = append(g.Warnings, "没有封面图")
	case 1:
		g.CoverPath = g.Covers[0].Path
	default:
		if c := pickNamedCover(g.Covers); c != "" && namedCoverCount(g.Covers) == 1 {
			g.CoverPath = c
			g.Warnings = append(g.Warnings, "存在多张图片，已按文件名选择封面，请确认")
			g.NeedsConfirm = true
		} else {
			g.Warnings = append(g.Warnings, "存在多张图片，请指定一张封面")
			g.NeedsConfirm = true
		}
	}
	for _, s := range g.Skipped {
		g.Warnings = append(g.Warnings, s)
	}
}

func pickNamedCover(covers []dramaFile) string {
	for _, c := range covers {
		stem := strings.ToLower(strings.TrimSuffix(c.Name, path.Ext(c.Name)))
		if stem == "cover" || stem == "folder" || stem == "thumb" || stem == "poster" {
			return c.Path
		}
	}
	return ""
}

func namedCoverCount(covers []dramaFile) int {
	n := 0
	for _, c := range covers {
		stem := strings.ToLower(strings.TrimSuffix(c.Name, path.Ext(c.Name)))
		if stem == "cover" || stem == "folder" || stem == "thumb" || stem == "poster" {
			n++
		}
	}
	return n
}

func zipMetasFromReader(r *zip.Reader) ([]zipMeta, error) {
	out := make([]zipMeta, 0, len(r.File))
	for _, f := range r.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("拒绝符号链接: %s", f.Name)
		}
		out = append(out, zipMeta{
			Name:             f.Name,
			CompressedSize:   int64(f.CompressedSize64),
			UncompressedSize: int64(f.UncompressedSize64),
			Mode:             f.Mode(),
		})
	}
	return out, nil
}
