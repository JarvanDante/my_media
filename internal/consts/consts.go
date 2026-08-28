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
)

// MinIO 对象前缀(桶 my-media)。视频 media/、漫画 comics/、动漫 cartoon/、抖音 video/ 同级。
const (
	PrefixMedia   = "media/"
	PrefixComics  = "comics/"
	PrefixCartoon = "cartoon/"
	PrefixVideo   = "video/"
)

func IsHLSKind(kind int) bool {
	return kind == KindVideo || kind == KindCartoon || kind == KindDouyin
}

func objectRoot(kind int) string {
	switch kind {
	case KindCartoon:
		return PrefixCartoon
	case KindDouyin:
		return PrefixVideo
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

