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
)

// MinIO 对象前缀(桶 my-media)。视频 media/、漫画 comics/、动漫 cartoon/ 同级。
const (
	PrefixMedia   = "media/"
	PrefixComics  = "comics/"
	PrefixCartoon = "cartoon/"
)

func IsHLSKind(kind int) bool {
	return kind == KindVideo || kind == KindCartoon
}

func SourceObjectPrefix(kind int, code string) string {
	root := PrefixMedia
	if kind == KindCartoon {
		root = PrefixCartoon
	}
	return root + "source/" + code + "/"
}

func HLSObjectPrefix(kind int, code string) string {
	root := PrefixMedia
	if kind == KindCartoon {
		root = PrefixCartoon
	}
	return root + "hls/" + code + "/"
}

