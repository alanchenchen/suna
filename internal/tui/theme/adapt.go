package theme

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// 颜色适配：把主题声明的颜色适配到当前终端背景。
//
// 适配原则：与终端背景匹配的一侧原样使用（开发者配色意图完整保留），
// 只有对比度不足时才调整亮度，且始终保留色相与饱和度（主题气质不变）。

const (
	// contrastTextRatio 是正文类前景色与背景的对比度下限（WCAG AA 正文标准）。
	contrastTextRatio = 4.5
	// contrastMutedRatio 是次要正文（帮助、描述、标签）的下限。
	// 它低于正文标准以保住层级，但仍是用户要读的文字，因此按 WCAG AA 正文标准取 4.5 的下限 4.0。
	contrastMutedRatio = 4.0
	// contrastDimRatio 是纯装饰色（边框、分隔线）的下限。
	// 它只防止「完全看不见」，不追求可读性：装饰色本就该低调，
	// 强行抬到正文标准会让用户写不出想要的暗色边框。
	contrastDimRatio = 2.0
)

// parseHex 解析 "#rrggbb"（大小写不敏感）。
func parseHex(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// formatHex 输出 "#rrggbb"。
func formatHex(c color.RGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// ColorString 把调色板颜色转成 glamour 能解析的颜色字符串。
//
// 必须走这里：Palette 里的颜色是 color.RGBA，直接 fmt.Sprint 会得到
// "{230 233 247 255}" 这类 glamour 无法解析的文本，渲染时会被静默回退成纯黑。
func ColorString(c color.Color) string {
	if rgba, ok := c.(color.RGBA); ok {
		return formatHex(rgba)
	}
	return fmt.Sprint(c)
}

// relativeLuminance 是 WCAG 2.x 相对亮度（0..1）。
func relativeLuminance(c color.RGBA) float64 {
	linear := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(c.R) + 0.7152*linear(c.G) + 0.0722*linear(c.B)
}

// contrastRatio 是 WCAG 对比度（1..21）。
func contrastRatio(a, b color.RGBA) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// isDarkColor 按相对亮度判断颜色深浅。
func isDarkColor(c color.RGBA) bool {
	return relativeLuminance(c) < 0.5
}

type hsl struct {
	H float64 // 0..360
	S float64 // 0..1
	L float64 // 0..1
}

func rgbToHSL(c color.RGBA) hsl {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	maxV := math.Max(r, math.Max(g, b))
	minV := math.Min(r, math.Min(g, b))
	l := (maxV + minV) / 2
	d := maxV - minV
	if d == 0 {
		return hsl{H: 0, S: 0, L: l}
	}
	var s float64
	if l > 0.5 {
		s = d / (2 - maxV - minV)
	} else {
		s = d / (maxV + minV)
	}
	var h float64
	switch maxV {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return hsl{H: h * 60, S: s, L: l}
}

func hslToRGB(v hsl) color.RGBA {
	h := math.Mod(v.H, 360)
	if h < 0 {
		h += 360
	}
	h /= 360
	s := math.Max(0, math.Min(1, v.S))
	l := math.Max(0, math.Min(1, v.L))
	if s == 0 {
		g := uint8(math.Round(l * 255))
		return color.RGBA{R: g, G: g, B: g, A: 0xff}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	channel := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 1.0/2:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}
	return color.RGBA{
		R: uint8(math.Round(channel(h+1.0/3) * 255)),
		G: uint8(math.Round(channel(h) * 255)),
		B: uint8(math.Round(channel(h-1.0/3) * 255)),
		A: 0xff,
	}
}

// ensureContrast 在保留色相/饱和度的前提下调整亮度，使前景与背景的对比度达标。
// 已经达标时原样返回：开发者在匹配的终端上看到的颜色与文件里写的一致。
func ensureContrast(fg, bg color.RGBA, minRatio float64) color.RGBA {
	if contrastRatio(fg, bg) >= minRatio {
		return fg
	}
	base := rgbToHSL(fg)
	darken := !isDarkColor(bg) // 浅底压暗、深底提亮
	best := fg
	bestRatio := contrastRatio(fg, bg)
	for step := 0.02; step <= 1.0; step += 0.02 {
		cand := base
		if darken {
			cand.L = math.Max(0, base.L-step)
		} else {
			cand.L = math.Min(1, base.L+step)
		}
		rgba := hslToRGB(cand)
		if ratio := contrastRatio(rgba, bg); ratio > bestRatio {
			best, bestRatio = rgba, ratio
		}
		if bestRatio >= minRatio {
			break
		}
	}
	return best
}

// deriveSurface 派生"表面"背景（代码块）：与终端背景保持可辨识但不突兀的差异。
// 这是背景色而非前景色，因此用固定亮度偏移，不能套用对比度逻辑——
// 否则浅色终端上的代码块会变成刺眼的深色块。
func deriveSurface(bg color.RGBA) color.RGBA {
	v := rgbToHSL(bg)
	if isDarkColor(bg) {
		v.L = math.Min(1, v.L+0.08)
	} else {
		v.L = math.Max(0, v.L-0.06)
	}
	return hslToRGB(v)
}

// onColor 返回色块上的文字色：按背景亮度选近黑或近白。
// 用于 tool/brand/guard 这类"色块 + 文字"样式，避免浅色主题里文字看不见。
func onColor(bg color.RGBA) color.RGBA {
	if relativeLuminance(bg) > 0.35 {
		return color.RGBA{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
	}
	return color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

// Background 是颜色适配的基准：终端背景的深浅，以及（可选的）真实颜色。
type Background struct {
	// Dark 是终端背景的深浅判断，决定适配方向与 markdown 风格。
	Dark bool
	// RGB 是检测到的真实终端背景色。零值表示终端不支持查询，
	// 此时按 Dark 取近似基准——深浅判断仍然有效，只是对比度基准略有偏差。
	RGB color.RGBA
}

// DarkBackground 与 LightBackground 是无真实背景色时的常用基准。
var (
	DarkBackground  = Background{Dark: true}
	LightBackground = Background{Dark: false}
)

// baseline 返回用于对比度计算的背景基准色。
// 优先用检测到的真实背景：对比度必须相对用户实际看到的底色计算，
// 否则偏亮的深色终端（如 Nord #2e3440）上，按近黑基准达标的颜色会实际不足。
func (b Background) baseline() color.RGBA {
	if b.RGB != (color.RGBA{}) {
		return b.RGB
	}
	if b.Dark {
		return color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}
	}
	return color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

// neutralDefaults 是 4 个可选中性色的缺省值：主题未声明时按终端背景派生。
// 语义色不在其中——它们由主题文件保证必填（见 validateColors）。
type neutralDefaults struct {
	text    color.RGBA
	muted   color.RGBA
	dim     color.RGBA
	surface color.RGBA
}

func neutralPalette(dark bool) neutralDefaults {
	if dark {
		return neutralDefaults{
			text:    color.RGBA{R: 0xcd, G: 0xd6, B: 0xf4, A: 0xff},
			muted:   color.RGBA{R: 0xa6, G: 0xad, B: 0xc8, A: 0xff},
			dim:     color.RGBA{R: 0x6c, G: 0x70, B: 0x86, A: 0xff},
			surface: color.RGBA{R: 0x31, G: 0x32, B: 0x44, A: 0xff},
		}
	}
	return neutralDefaults{
		text:    color.RGBA{R: 0x24, G: 0x29, B: 0x2e, A: 0xff},
		muted:   color.RGBA{R: 0x5c, G: 0x63, B: 0x70, A: 0xff},
		dim:     color.RGBA{R: 0x8b, G: 0x94, B: 0x9e, A: 0xff},
		surface: color.RGBA{R: 0xf0, G: 0xf2, B: 0xf5, A: 0xff},
	}
}

// Adapt 把主题的原始颜色适配到指定终端背景。
//
// 规则：与终端背景匹配的一侧原样使用（开发者配色意图完整保留），
// 只有对比度不足时才调整亮度，且保留色相与饱和度。
func Adapt(name string, c Colors, bg Background) Palette {
	baseline := bg.baseline()
	dark := bg.Dark

	// 语义色由主题文件保证必填；兜底用内置 default，仅防御异常调用。
	def := DefaultColors()
	parse := func(v, fallback string) color.RGBA {
		if rgba, ok := parseHex(v); ok {
			return rgba
		}
		rgba, _ := parseHex(fallback)
		return rgba
	}

	accent := parse(c.Accent, def.Accent)
	success := parse(c.Success, def.Success)
	info := parse(c.Info, def.Info)
	warning := parse(c.Warning, def.Warning)
	errColor := parse(c.Error, def.Error)

	// 中性色可选：缺省时按终端背景派生（深底亮灰系、浅底暗灰系）。
	neutrals := neutralPalette(dark)
	text := parse(c.Text, formatHex(neutrals.text))
	muted := parse(c.Muted, formatHex(neutrals.muted))
	dim := parse(c.Dim, formatHex(neutrals.dim))
	surface := parse(c.Surface, formatHex(neutrals.surface))

	// 语义色：需要与背景有足够对比才能作为文字/图标使用。
	accent = ensureContrast(accent, baseline, contrastTextRatio)
	success = ensureContrast(success, baseline, contrastTextRatio)
	info = ensureContrast(info, baseline, contrastTextRatio)
	warning = ensureContrast(warning, baseline, contrastTextRatio)
	errColor = ensureContrast(errColor, baseline, contrastTextRatio)

	// 中性色：text 用正文标准；muted 是次要正文，用较低标准保住层级；
	// dim 是纯装饰，只防「完全看不见」。
	//
	// 中性色走 ensureContrastNeutral：保留绝对彩度而非 HSL 饱和度，
	// 否则近白的浅蓝正文压暗后会变成高饱和蓝，让界面整体染上蓝调。
	text = ensureContrastNeutral(text, baseline, contrastTextRatio)
	muted = ensureContrastNeutral(muted, baseline, contrastMutedRatio)
	dim = ensureContrastNeutral(dim, baseline, contrastDimRatio)
	surface = adaptSurface(surface, baseline)

	return Palette{
		Name:    name,
		Dark:    dark,
		Accent:  accent,
		Success: success,
		Info:    info,
		Warning: warning,
		Error:   errColor,
		Text:    text,
		Muted:   muted,
		Dim:     dim,
		Surface: surface,

		OnAccent:  onColor(accent),
		OnSuccess: onColor(success),
		OnWarning: onColor(warning),
		OnError:   onColor(errColor),
	}
}

// adaptSurface 把主题声明的表面色适配到当前终端背景。
// 这是背景色而非前景色：目标是「与终端背景可辨识但不突兀」，
// 不能套用前景色的对比度逻辑，否则浅色终端上的代码块会变成刺眼的深色块。
// 主题未声明 surface 时按终端背景派生。
func adaptSurface(surface, bg color.RGBA) color.RGBA {
	if surface == (color.RGBA{}) {
		return deriveSurface(bg)
	}
	// 与终端背景差异过小时推开，避免代码块「看不见」；差异过大时拉回，避免刺眼。
	ratio := contrastRatio(surface, bg)
	if ratio < 1.06 {
		return deriveSurface(bg)
	}
	if ratio > 3.2 {
		v := rgbToHSL(surface)
		if isDarkColor(bg) {
			v.L = math.Min(1, v.L-0.10)
		} else {
			v.L = math.Max(0, v.L+0.10)
		}
		return hslToRGB(v)
	}
	return surface
}
