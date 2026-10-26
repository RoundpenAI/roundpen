package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	_ "image/gif"
	"io"
	"path"
	"strings"
)

// MaxImageBytes caps raw image bytes before decode (host or workspace).
const MaxImageBytes = 8 << 20 // 8 MiB

// MaxImageEdge caps the longest side after optional downscale for vision.
const MaxImageEdge = 2048

// VisionPayloadKey is present in ReadImage JSON when bytes were attached for the model.
const VisionPayloadKey = "_vision"

// RegisterReadImage adds ReadImage for photos under /workspace or granted host dirs,
// plus ListDirectoryGrants so the agent can see authorized NAS roots.
func RegisterReadImage(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "ReadImage",
		Description: "Read an image (jpeg/png/gif/webp) from the Agent workspace or a granted host " +
			"directory for visual analysis. Prefer this over Read for photos. Paths outside grants are refused. " +
			"Use ListDirectoryGrants to see authorized roots; Glob to find files.",
		Parameters: objectSchema(map[string]any{
			"file_path": map[string]any{"type": "string", "description": "Absolute path under /workspace or a granted host directory"},
		}, "file_path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.FilePath) == "" {
				return "", fmt.Errorf("file_path is required")
			}
			return binder.readImage(ctx, actor, in.FilePath)
		},
	})
	r.Register(Tool{
		Name: "ListDirectoryGrants",
		Description: "List host directories this assistant is authorized to read (NAS shares). " +
			"Empty means only /workspace is available until the user adds grants in the assistant detail page.",
		Parameters: objectSchema(map[string]any{}),
		Call: func(ctx context.Context, actor Actor, _ json.RawMessage) (string, error) {
			return binder.listDirectoryGrants(ctx, actor)
		},
	})
}

func (b *AgentBinder) listDirectoryGrants(ctx context.Context, actor Actor) (string, error) {
	if b.Host == nil {
		return `{"grants":[],"note":"host directory grants are not available in this session"}`, nil
	}
	grants, err := b.Host.ListGrants(ctx, actor)
	if err != nil {
		return "", err
	}
	if grants == nil {
		grants = []DirGrantView{}
	}
	raw, err := json.Marshal(map[string]any{"grants": grants})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (b *AgentBinder) readImage(ctx context.Context, actor Actor, filePath string) (string, error) {
	rc, absLabel, err := b.openImageSource(ctx, actor, filePath)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	raw, err := io.ReadAll(io.LimitReader(rc, MaxImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("%s is empty", absLabel)
	}
	if len(raw) > MaxImageBytes {
		return "", fmt.Errorf("%s exceeds %d byte limit", absLabel, MaxImageBytes)
	}

	mime := sniffImageMIME(raw, absLabel)
	if mime == "" {
		return "", fmt.Errorf("%s is not a supported image (jpeg/png/gif/webp)", absLabel)
	}

	outMIME := mime
	outBytes := raw
	if mime != "image/webp" {
		if resized, rMIME, rErr := maybeDownscaleImage(raw); rErr == nil && len(resized) > 0 {
			outBytes, outMIME = resized, rMIME
		}
	}

	payload := map[string]any{
		"ok":    true,
		"path":  absLabel,
		"mime":  outMIME,
		"bytes": len(outBytes),
		"note":  "image attached for vision on the next model step",
		VisionPayloadKey: map[string]any{
			"mime":   outMIME,
			"base64": base64.StdEncoding.EncodeToString(outBytes),
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (b *AgentBinder) openImageSource(ctx context.Context, actor Actor, filePath string) (io.ReadCloser, string, error) {
	if abs, isHost, err := hostAbsPath(filePath); err != nil {
		return nil, "", err
	} else if isHost {
		if b.Host == nil {
			return nil, "", fmt.Errorf("host directories are not available in this session")
		}
		rc, fi, err := b.Host.OpenHost(ctx, actor, abs)
		if err != nil {
			return nil, "", err
		}
		if fi != nil && fi.IsDir() {
			_ = rc.Close()
			return nil, "", fmt.Errorf("%s is a directory — use Glob with path=%q", abs, abs)
		}
		return rc, abs, nil
	}
	rel, err := b.resolveRel(filePath)
	if err != nil {
		return nil, "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return nil, "", err
	}
	if b.Files == nil {
		return nil, "", fmt.Errorf("workspace files not configured")
	}
	rc, err := b.Files.ReadFile(ctx, id, rel)
	if err != nil {
		return nil, "", fmt.Errorf("read file: %w", err)
	}
	label := filePath
	if !strings.HasPrefix(filePath, "/") {
		label = path.Join(WorkspaceRoot, rel)
	}
	return rc, label, nil
}

func sniffImageMIME(raw []byte, name string) string {
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff {
		return "image/jpeg"
	}
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(raw) >= 6 && (bytes.Equal(raw[:6], []byte("GIF87a")) || bytes.Equal(raw[:6], []byte("GIF89a"))) {
		return "image/gif"
	}
	if len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

func maybeDownscaleImage(raw []byte) ([]byte, string, error) {
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, "", fmt.Errorf("empty image")
	}
	if w <= MaxImageEdge && h <= MaxImageEdge {
		return nil, "", fmt.Errorf("no resize needed")
	}
	scale := float64(MaxImageEdge) / float64(w)
	if h > w {
		scale = float64(MaxImageEdge) / float64(h)
	}
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := scaleNearest(img, nw, nh)
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/jpeg", nil
	default:
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "image/png", nil
	}
}

func scaleNearest(src image.Image, nw, nh int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := sb.Min.Y + y*sh/nh
		for x := 0; x < nw; x++ {
			sx := sb.Min.X + x*sw/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// StripVisionPayload removes the heavy base64 blob from a ReadImage tool result
// for the tool-role message, returning a slim summary and optional vision parts.
func StripVisionPayload(toolName, result string) (summary string, mime, b64 string, ok bool) {
	if toolName != "ReadImage" || strings.TrimSpace(result) == "" {
		return result, "", "", false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return result, "", "", false
	}
	vis, _ := payload[VisionPayloadKey].(map[string]any)
	if vis == nil {
		return result, "", "", false
	}
	mime, _ = vis["mime"].(string)
	b64, _ = vis["base64"].(string)
	delete(payload, VisionPayloadKey)
	slim, err := json.Marshal(payload)
	if err != nil || mime == "" || b64 == "" {
		return result, "", "", false
	}
	return string(slim), mime, b64, true
}
