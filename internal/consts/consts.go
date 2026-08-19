package consts

const (
	AssetStatusDraft      = 0
	AssetStatusProcessing = 1
	AssetStatusReady      = 2
	AssetStatusFailed     = 3
	AssetStatusOff        = 4
)

const (
	KindVideo  = 0
	KindComics = 1
)

// MinIO 对象前缀(桶 my-media)。视频走 media/，漫画与它同级走 comics/。
const (
	PrefixMedia  = "media/"
	PrefixComics = "comics/"
)

