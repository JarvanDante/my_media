package consts

const (
	AssetStatusDraft      = 0
	AssetStatusProcessing = 1
	AssetStatusReady      = 2
	AssetStatusFailed     = 3
	AssetStatusOff        = 4
)

const (
	KindVideo   = 0
	KindComics  = 1
	KindCartoon = 2
	KindDouyin  = 3
	KindNovel   = 4
	KindDrama   = 5
)

const (
	UpdateStatusNone    = 0
	UpdateStatusSerial  = 1
	UpdateStatusEnded   = 2
)

// MinIO 对象前缀(桶 my-media)。视频 media/、漫画 comics/、动漫 cartoon/、抖音 video/、小说封面 novels/、短剧 drama/ 同级。
const (
	PrefixMedia   = "media/"
	PrefixComics  = "comics/"
	PrefixCartoon = "cartoon/"
	PrefixVideo   = "video/"
	PrefixNovel   = "novels/"
	PrefixDrama   = "drama/"
)

func IsChapterKind(kind int) bool {
	return kind == KindComics || kind == KindNovel
}

func IsHLSKind(kind int) bool {
	return kind == KindVideo || kind == KindCartoon || kind == KindDouyin
}

func objectRoot(kind int) string {
	switch kind {
	case KindCartoon:
		return PrefixCartoon
	case KindDouyin:
		return PrefixVideo
	case KindDrama:
		return PrefixDrama
	default:
		return PrefixMedia
	}
}

func SourceObjectPrefix(kind int, code string) string {
	return objectRoot(kind) + "source/" + code + "/"
}

func HLSObjectPrefix(kind int, code string) string {
	return objectRoot(kind) + "hls/" + code + "/"
}

func IsDramaKind(kind int) bool { return kind == KindDrama }

func IsDramaSeries(kind int, parentID int64) bool {
	return kind == KindDrama && parentID == 0
}

func IsDramaEpisode(kind int, parentID int64) bool {
	return kind == KindDrama && parentID > 0
}

func DramaCoverKey(code string) string { return PrefixDrama + code + "/cover.bnc" }

