// video-images-web: 只读 S3 服务代理 + 内网画廊。
//
//	公网端口(:80) —— 只服务图片对象(带 CORS,照搬原 nginx);根路径/画廊不给(404)。CF 回源到这。
//	内网端口(:8090) —— 画廊看板(/)+ 图片 + /api/list;走内网 VS,不挂 CF。
//
// 画廊读 config.ini 与桶对象做对比,配了但没图的流也显示(红占位)。
package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"golang.org/x/sync/singleflight"
)

var (
	client       *minio.Client
	bucket       string
	cacheDir     string
	ttl          time.Duration
	staleSeconds int
	configFile   string
	publicBase   string // 画廊"外网"链接用,来自 chart(ingressGateway.host 派生或 publicBaseUrl 覆盖)
	group        singleflight.Group

	// CORS(照搬原 nginx,全走环境变量)
	corsOrigin  string
	corsTiming  string
	corsMethods string
	corsHeaders string

	listMu    sync.Mutex
	listCache []objInfo
	listAt    time.Time
	listTTL   time.Duration

	// 404 负缓存:最近确认不存在的 key 直接 404,不再穿透打 MinIO(省按次计费的 op)
	negMu    sync.Mutex
	negCache map[string]time.Time
	negTTL   time.Duration
)

const negCacheMax = 10000 // 负缓存条数上限,超了整体清空,防坏 URL 灌爆内存

// negHit 命中未过期的负缓存(该 key 最近确认不存在)
func negHit(key string) bool {
	if negTTL <= 0 {
		return false
	}
	negMu.Lock()
	defer negMu.Unlock()
	t, ok := negCache[key]
	if !ok {
		return false
	}
	if time.Since(t) >= negTTL {
		delete(negCache, key)
		return false
	}
	return true
}

// negPut 记下"该 key 不存在"
func negPut(key string) {
	if negTTL <= 0 {
		return
	}
	negMu.Lock()
	if len(negCache) >= negCacheMax {
		negCache = map[string]time.Time{}
	}
	negCache[key] = time.Now()
	negMu.Unlock()
}

type objInfo struct {
	Name     string `json:"name"`
	Modified int64  `json:"modified"` // unix 秒;无图为 0
	Size     int64  `json:"size"`
	HasImage bool   `json:"has_image"` // 桶里是否有图(配了但从没出图的为 false)
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[video-images-web] ")

	endpoint := env("MINIO_ENDPOINT", "devops-minio.devops.svc.cluster.local:9000")
	bucket = env("MINIO_BUCKET", "video-images")
	cacheDir = env("CACHE_DIR", "/var/cache/video-images")
	ttl = time.Duration(envInt("CACHE_TTL_SECONDS", 30)) * time.Second
	staleSeconds = envInt("GALLERY_STALE_SECONDS", 300)
	listTTL = time.Duration(envInt("LIST_CACHE_SECONDS", 10)) * time.Second
	negTTL = time.Duration(envInt("NEG_CACHE_SECONDS", 10)) * time.Second
	negCache = map[string]time.Time{}
	configFile = env("CONFIG_FILE", "/etc/video-images/config.ini")
	publicBase = strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	corsOrigin = env("CORS_ALLOW_ORIGIN", "*")
	corsTiming = env("CORS_TIMING_ORIGIN", "*")
	corsMethods = env("CORS_ALLOW_METHODS", "GET, POST, OPTIONS, DELETE")
	corsHeaders = env("CORS_ALLOW_HEADERS", "reqid, nid, host, x-real-ip, x-forwarded-ip, event-type, event-id, accept, content-type")
	publicAddr := env("LISTEN", ":80")
	adminAddr := env("ADMIN_LISTEN", ":8090")
	useSSL, _ := strconv.ParseBool(env("MINIO_USE_SSL", "false"))

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Fatalf("创建缓存目录失败: %v", err)
	}
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), ""),
		Secure: useSSL,
	})
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}
	client = c

	// 公网:只给图片(带 CORS),根路径 404
	pub := http.NewServeMux()
	pub.HandleFunc("/healthz", healthz)
	pub.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions { // CORS 预检
			setCORS(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/" {
			http.NotFound(w, r)
			return
		}
		serveImage(w, r)
	})
	go func() {
		log.Printf("INFO 公网图片端口 %s (CORS Origin=%s)", publicAddr, corsOrigin)
		s := &http.Server{Addr: publicAddr, Handler: pub, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}
		log.Fatal(s.ListenAndServe())
	}()

	// 内网:画廊 + 图片 + api
	adm := http.NewServeMux()
	adm.HandleFunc("/healthz", healthz)
	adm.HandleFunc("/api/list", apiList)
	adm.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/" {
			galleryPage(w, r)
			return
		}
		serveImage(w, r)
	})
	log.Printf("INFO 内网画廊端口 %s (桶=%s 卡住阈值=%ds 公网base=%q)", adminAddr, bucket, staleSeconds, publicBase)
	s := &http.Server{Addr: adminAddr, Handler: adm, ReadTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func healthz(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) }

func setCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", corsOrigin)
	h.Set("Timing-Allow-Origin", corsTiming)
	h.Set("Access-Control-Allow-Methods", corsMethods)
	h.Set("Access-Control-Allow-Headers", corsHeaders)
}

// ---------- 图片服务(带缓存 + CORS) ----------

func serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	setCORS(w)
	key := strings.TrimPrefix(r.URL.Path, "/")
	if key == "" || strings.Contains(key, "..") || strings.ContainsAny(key, "\\") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cachePath := filepath.Join(cacheDir, filepath.FromSlash(key))
	if fi, err := os.Stat(cachePath); err == nil && time.Since(fi.ModTime()) < ttl {
		writeFile(w, r, cachePath, "HIT")
		return
	}
	// 负缓存:最近确认不存在的 key 直接 404,不打 MinIO(反复请求缺图的流/坏 URL 时省 op)
	if negHit(key) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// 用独立 context(不绑单个请求):singleflight 的"带头"请求若中途断连,
	// 不会把取消错误传染给其他共享者(fetch 内部已有 10s 超时兜底)
	_, err, _ := group.Do(key, func() (interface{}, error) {
		return nil, fetch(context.Background(), key, cachePath)
	})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			negPut(key) // 记下不存在,后续同 key 直接 404 不再打 MinIO
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if _, e := os.Stat(cachePath); e == nil {
			log.Printf("WARN 回源失败,降级旧缓存 %s: %v", key, err)
			writeFile(w, r, cachePath, "STALE")
			return
		}
		log.Printf("ERROR 回源失败 %s: %v", key, err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	writeFile(w, r, cachePath, "MISS")
}

func fetch(ctx context.Context, key, cachePath string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	obj, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()
	if _, err := obj.Stat(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	tmp := cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, obj); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	_ = f.Close()
	return os.Rename(tmp, cachePath)
}

func writeFile(w http.ResponseWriter, r *http.Request, path, status string) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(int(ttl.Seconds())))
	w.Header().Set("X-Cache", status)
	http.ServeFile(w, r, path)
}

// ---------- 画廊 ----------

// apiList 返回 config 全量流 ✕ 桶对象 合并(配了但没图的 has_image=false),带短缓存
func apiList(w http.ResponseWriter, r *http.Request) {
	listMu.Lock()
	fresh := time.Since(listAt) < listTTL && listCache != nil
	items := listCache
	listMu.Unlock()

	if !fresh {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		inBucket := map[string]objInfo{}
		for o := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if o.Err != nil {
				http.Error(w, "list error: "+o.Err.Error(), http.StatusBadGateway)
				return
			}
			inBucket[o.Key] = objInfo{Name: o.Key, Modified: o.LastModified.Unix(), Size: o.Size, HasImage: true}
		}
		configNames := ParseConfigNames(configFile)

		var got []objInfo
		seen := map[string]bool{}
		// 先按配置全量(含从没出图的)
		for name := range configNames {
			seen[name] = true
			if oi, ok := inBucket[name]; ok {
				got = append(got, oi)
			} else {
				got = append(got, objInfo{Name: name, HasImage: false}) // 配了但没图
			}
		}
		// 桶里有但配置里没有的(清理器一般会删,兜底也显示)
		for name, oi := range inBucket {
			if !seen[name] {
				got = append(got, oi)
			}
		}

		listMu.Lock()
		listCache, listAt = got, time.Now()
		items = got
		listMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"now":           time.Now().Unix(),
		"stale_seconds": staleSeconds,
		"public_base":   publicBase,
		"items":         items,
	})
}

func galleryPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(galleryHTML))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
