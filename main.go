package main

import (
	"github.com/gogf/gf/v2/os/gctx"

	"github.com/JarvanDante/my_media/internal/cmd"
)

func main() {
	cmd.Main.Run(gctx.GetInitCtx())
}
