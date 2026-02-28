package texeluidemo

import (
	texeluidemo "github.com/framegrace/texelui/apps/texelui-demo"
	"github.com/framegrace/texelation/registry"
)

func init() {
	registry.RegisterBuiltInProvider(func(_ *registry.Registry) (*registry.Manifest, registry.AppFactory) {
		return &registry.Manifest{
			Name:        "texelui-demo",
			DisplayName: "TexelUI Demo",
			Description: "Widget showcase with graphics demo",
			Icon:        "D",
			Category:    "demo",
		}, func() interface{} {
			return texeluidemo.New()
		}
	})
}
