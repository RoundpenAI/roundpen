package nasscene

import "github.com/RoundpenAI/roundpen/internal/assistant"

// Template keys for the NAS scene pack (stable product ids).
const (
	KeyPhotos = "nas.photos"
	KeyFiles  = "nas.files"
	KeyMovies = "nas.movies"
	KeyMusic  = "nas.music"
)

// Template describes one seed assistant.
type Template struct {
	Key         string
	Name        string
	Bio         string
	Caps        assistant.Capabilities
	NetworkTier string
}

// Templates is the ordered NAS scene pack.
func Templates() []Template {
	return []Template{
		{
			Key:  KeyPhotos,
			Name: "相册助手",
			Bio:  "整理家庭相册：往年今日 / N 年前今日，挑选回忆并拷到目标目录。先确认目录授权，批量改动前给出计划。",
			Caps: assistant.Capabilities{Shell: true, Browser: true},
			NetworkTier: assistant.NetworkNone,
		},
		{
			Key:  KeyFiles,
			Name: "文件助手",
			Bio:  "整理 NAS 文件：批量重命名、查重、目录重规划。只用 Host* / HostApplyPlan，先 dry-run 计划再等人确认。",
			Caps: assistant.Capabilities{Shell: true, Browser: true},
			NetworkTier: assistant.NetworkNone,
		},
		{
			Key:  KeyMovies,
			Name: "影视助手",
			Bio:  "搜源与补全片库缺口，确认后投递到已配置的下载器；播放只返回路径或系统深链。不内置片源站目录。",
			Caps: assistant.Capabilities{Shell: true, Browser: true},
			NetworkTier: assistant.NetworkAll,
		},
		{
			Key:  KeyMusic,
			Name: "音乐助手",
			Bio:  "搜源与补全音乐库缺口，确认后投递到已配置的下载器。与影视助手共享下载后端配置。",
			Caps: assistant.Capabilities{Shell: true, Browser: true},
			NetworkTier: assistant.NetworkAll,
		},
	}
}
