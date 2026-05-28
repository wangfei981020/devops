package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"opsplatform-gke-version-backend/metrics"
	"opsplatform-gke-version-backend/models"
)

type Scraper struct {
	db          *sql.DB
	alertEngine *AlertEngine
	mu          sync.RWMutex
	interval    time.Duration
	ticker      *time.Ticker
	stop        chan struct{}
}

func NewScraper(db *sql.DB, initialInterval time.Duration, alertEngine *AlertEngine) *Scraper {
	return &Scraper{
		db:          db,
		alertEngine: alertEngine,
		interval:    initialInterval,
		stop:        make(chan struct{}),
	}
}

func (s *Scraper) Start() {
	s.mu.Lock()
	s.ticker = time.NewTicker(s.interval)
	s.mu.Unlock()
	go s.loop()
}

func (s *Scraper) SetInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d == s.interval {
		return
	}
	log.Printf("scraper interval changed: %v -> %v", s.interval, d)
	s.interval = d
	if s.ticker != nil {
		s.ticker.Reset(d)
	}
}

func (s *Scraper) loop() {
	s.ScrapeAll(context.Background())
	for {
		s.mu.RLock()
		c := s.ticker.C
		s.mu.RUnlock()
		select {
		case <-c:
			s.ScrapeAll(context.Background())
		case <-s.stop:
			return
		}
	}
}

func (s *Scraper) Stop() { close(s.stop) }

func (s *Scraper) ScrapeAll(ctx context.Context) {
	clusters, err := s.listEnabledClusters()
	if err != nil {
		log.Printf("listClusters: %v", err)
		return
	}
	for _, cl := range clusters {
		if err := s.ScrapeOne(ctx, cl); err != nil {
			log.Printf("scrape %s/%s/%s: %v", cl.ProjectID, cl.Location, cl.Name, err)
			s.saveError(cl.ID, err.Error())
		}
	}
	if s.alertEngine != nil {
		s.alertEngine.Evaluate()
	}
}

func (s *Scraper) ScrapeOne(ctx context.Context, cl *models.Cluster) error {
	if cl.SAKeyJSON == "" {
		return fmt.Errorf("cluster %s/%s/%s has no SA key configured", cl.ProjectID, cl.Location, cl.Name)
	}
	gcp, err := NewGCPClientWithJSON(ctx, []byte(cl.SAKeyJSON))
	if err != nil {
		return fmt.Errorf("init GCP client: %w", err)
	}
	defer gcp.Close()

	srv, err := gcp.GetServerConfig(ctx, cl.ProjectID, cl.Location)
	if err != nil {
		return err
	}
	info, err := gcp.GetCluster(ctx, cl.ProjectID, cl.Location, cl.Name)
	if err != nil {
		return err
	}

	snap := buildSnapshot(cl, info, srv)

	writeClusterMetrics(cl, snap)
	for _, np := range snap.NodePools {
		writeNodepoolMetrics(cl, np)
	}

	if err := s.saveSnapshot(snap); err != nil {
		return err
	}

	// 检测版本变更并写入 history（集群+每个节点池）
	RecordTransition(s.db, cl, snap)
	return nil
}

// AlertEngine 暴露给 handler 调用（如单集群手动刷新后触发评估）
func (s *Scraper) AlertEngine() *AlertEngine { return s.alertEngine }

func buildSnapshot(cl *models.Cluster, info *ClusterInfo, srv *ServerConfig) *models.ClusterSnapshot {
	cur := info.CurrentMasterVersion
	maxUp := pickMaxUpgradable(cur, srv.ValidMasterVersions)
	latest := ""
	if len(srv.ValidMasterVersions) > 0 {
		latest = srv.ValidMasterVersions[0]
	}

	cm := IndexDiff(cur, maxUp, srv.ValidMasterVersions)
	cmd, _ := ArithmeticDiff(cur, maxUp)
	ml := IndexDiff(maxUp, latest, srv.ValidMasterVersions)
	mld, _ := ArithmeticDiff(maxUp, latest)
	cl2 := IndexDiff(cur, latest, srv.ValidMasterVersions)
	cld, _ := ArithmeticDiff(cur, latest)

	stdEnd, extEnd := ResolveEOL(cur, srv.EOL)

	nps := []models.NodePoolInfo{}
	for _, np := range info.NodePools {
		nv := np.GetVersion()
		nmax := pickMaxUpgradableNode(cur, srv.ValidNodeVersions)
		nlatest := ""
		if len(srv.ValidNodeVersions) > 0 {
			nlatest = srv.ValidNodeVersions[0]
		}
		stdN, extN := ResolveEOL(nv, srv.EOL)
		nps = append(nps, models.NodePoolInfo{
			Name:                          np.GetName(),
			CurrentVersion:                nv,
			MaxUpgradableVersion:          nmax,
			LatestAvailableVersion:        nlatest,
			CurrentToMaxVersionsBehind:    IndexDiff(nv, nmax, srv.ValidNodeVersions),
			CurrentToMaxVersionDiff:       safeDiff(nv, nmax),
			MaxToLatestVersionsBehind:     IndexDiff(nmax, nlatest, srv.ValidNodeVersions),
			MaxToLatestVersionDiff:        safeDiff(nmax, nlatest),
			CurrentToLatestVersionsBehind: IndexDiff(nv, nlatest, srv.ValidNodeVersions),
			CurrentToLatestVersionDiff:    safeDiff(nv, nlatest),
			StdSupportEnd:                 stdN,
			ExtSupportEnd:                 extN,
		})
	}

	now := time.Now()
	return &models.ClusterSnapshot{
		ClusterID:                     cl.ID,
		CurrentVersion:                cur,
		MaxUpgradableVersion:          maxUp,
		LatestAvailableVersion:        latest,
		CurrentToMaxVersionsBehind:    cm,
		CurrentToMaxVersionDiff:       cmd,
		MaxToLatestVersionsBehind:     ml,
		MaxToLatestVersionDiff:        mld,
		CurrentToLatestVersionsBehind: cl2,
		CurrentToLatestVersionDiff:    cld,
		StdSupportEnd:                 stdEnd,
		ExtSupportEnd:                 extEnd,
		NodePools:                     nps,
		LastRefreshedAt:               &now,
	}
}

func safeDiff(low, high string) float64 {
	v, _ := ArithmeticDiff(low, high)
	return v
}

func pickMaxUpgradable(current string, valid []string) string {
	if len(valid) == 0 {
		return current
	}
	return valid[0]
}

func pickMaxUpgradableNode(currentMaster string, validNode []string) string {
	if len(validNode) == 0 {
		return currentMaster
	}
	latest := validNode[0]
	mv, err1 := ParseVersion(latest)
	cm, err2 := ParseVersion(currentMaster)
	if err1 != nil || err2 != nil {
		return latest
	}
	if compareVer(cm, mv) < 0 {
		return currentMaster
	}
	return latest
}

func (s *Scraper) listEnabledClusters() ([]*models.Cluster, error) {
	rows, err := s.db.Query(`SELECT id, project_id, location, name, COALESCE(sa_key_json, ''), enabled, created_at, updated_at FROM clusters WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.Cluster{}
	for rows.Next() {
		c := &models.Cluster{}
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Location, &c.Name, &c.SAKeyJSON, &c.Enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.HasSAKey = c.SAKeyJSON != ""
		out = append(out, c)
	}
	return out, nil
}

func (s *Scraper) saveSnapshot(snap *models.ClusterSnapshot) error {
	npJSON, _ := json.Marshal(snap.NodePools)
	_, err := s.db.Exec(`
		REPLACE INTO cluster_snapshots
		(cluster_id, current_version, max_upgradable_version, latest_available_version,
		 current_to_max_versions_behind, current_to_max_version_diff,
		 max_to_latest_versions_behind, max_to_latest_version_diff,
		 current_to_latest_versions_behind, current_to_latest_version_diff,
		 std_support_end, ext_support_end, nodepools_json, last_refreshed_at, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, '')
	`,
		snap.ClusterID, snap.CurrentVersion, snap.MaxUpgradableVersion, snap.LatestAvailableVersion,
		snap.CurrentToMaxVersionsBehind, snap.CurrentToMaxVersionDiff,
		snap.MaxToLatestVersionsBehind, snap.MaxToLatestVersionDiff,
		snap.CurrentToLatestVersionsBehind, snap.CurrentToLatestVersionDiff,
		snap.StdSupportEnd, snap.ExtSupportEnd, npJSON, snap.LastRefreshedAt)
	return err
}

// SaveError 给手动 refresh 路径用：把单集群抓取错误写到 cluster_snapshots.last_error，
// 便于前端展示和 DB 排查。
func (s *Scraper) SaveError(clusterID int, msg string) { s.saveError(clusterID, msg) }

func (s *Scraper) saveError(clusterID int, msg string) {
	now := time.Now()
	_, _ = s.db.Exec(`
		INSERT INTO cluster_snapshots (cluster_id, last_refreshed_at, last_error)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE last_refreshed_at=VALUES(last_refreshed_at), last_error=VALUES(last_error)
	`, clusterID, now, msg)
}

func writeClusterMetrics(cl *models.Cluster, snap *models.ClusterSnapshot) {
	lbl := []string{cl.ProjectID, cl.Location, cl.Name}
	metrics.ClusterInfo.WithLabelValues(append(lbl,
		snap.CurrentVersion, snap.MaxUpgradableVersion, snap.LatestAvailableVersion)...).Set(1)
	metrics.ClusterCurToMaxBehind.WithLabelValues(lbl...).Set(float64(snap.CurrentToMaxVersionsBehind))
	metrics.ClusterCurToMaxDiff.WithLabelValues(lbl...).Set(snap.CurrentToMaxVersionDiff)
	metrics.ClusterMaxToLatestBehind.WithLabelValues(lbl...).Set(float64(snap.MaxToLatestVersionsBehind))
	metrics.ClusterMaxToLatestDiff.WithLabelValues(lbl...).Set(snap.MaxToLatestVersionDiff)
	metrics.ClusterCurToLatestBehind.WithLabelValues(lbl...).Set(float64(snap.CurrentToLatestVersionsBehind))
	metrics.ClusterCurToLatestDiff.WithLabelValues(lbl...).Set(snap.CurrentToLatestVersionDiff)
	if t, ok := parseDate(snap.StdSupportEnd); ok {
		metrics.ClusterStdEnd.WithLabelValues(lbl...).Set(float64(t.Unix()))
	}
	if t, ok := parseDate(snap.ExtSupportEnd); ok {
		metrics.ClusterExtEnd.WithLabelValues(lbl...).Set(float64(t.Unix()))
	}
	metrics.ExporterLastScrape.WithLabelValues(cl.Name).Set(float64(time.Now().Unix()))
}

func writeNodepoolMetrics(cl *models.Cluster, np models.NodePoolInfo) {
	lbl := []string{cl.ProjectID, cl.Location, cl.Name, np.Name}
	metrics.NodepoolInfo.WithLabelValues(append(lbl,
		np.CurrentVersion, np.MaxUpgradableVersion, np.LatestAvailableVersion)...).Set(1)
	metrics.NodepoolCurToMaxBehind.WithLabelValues(lbl...).Set(float64(np.CurrentToMaxVersionsBehind))
	metrics.NodepoolCurToMaxDiff.WithLabelValues(lbl...).Set(np.CurrentToMaxVersionDiff)
	metrics.NodepoolMaxToLatestBehind.WithLabelValues(lbl...).Set(float64(np.MaxToLatestVersionsBehind))
	metrics.NodepoolMaxToLatestDiff.WithLabelValues(lbl...).Set(np.MaxToLatestVersionDiff)
	metrics.NodepoolCurToLatestBehind.WithLabelValues(lbl...).Set(float64(np.CurrentToLatestVersionsBehind))
	metrics.NodepoolCurToLatestDiff.WithLabelValues(lbl...).Set(np.CurrentToLatestVersionDiff)
	if t, ok := parseDate(np.StdSupportEnd); ok {
		metrics.NodepoolStdEnd.WithLabelValues(lbl...).Set(float64(t.Unix()))
	}
	if t, ok := parseDate(np.ExtSupportEnd); ok {
		metrics.NodepoolExtEnd.WithLabelValues(lbl...).Set(float64(t.Unix()))
	}
}

func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
