package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	ClusterInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gke_cluster_info",
		Help: "GKE cluster version info (label-only).",
	}, []string{"project", "location", "cluster", "current_version", "max_upgradable_version", "latest_available_version"})

	ClusterCurToMaxBehind    = newGauge("gke_cluster_current_to_max_versions_behind", "current → max_upgradable index diff", "project", "location", "cluster")
	ClusterCurToMaxDiff      = newGauge("gke_cluster_current_to_max_version_diff", "current → max_upgradable arithmetic diff", "project", "location", "cluster")
	ClusterMaxToLatestBehind = newGauge("gke_cluster_max_to_latest_versions_behind", "max_upgradable → latest index diff", "project", "location", "cluster")
	ClusterMaxToLatestDiff   = newGauge("gke_cluster_max_to_latest_version_diff", "max_upgradable → latest arithmetic diff", "project", "location", "cluster")
	ClusterCurToLatestBehind = newGauge("gke_cluster_current_to_latest_versions_behind", "current → latest index diff", "project", "location", "cluster")
	ClusterCurToLatestDiff   = newGauge("gke_cluster_current_to_latest_version_diff", "current → latest arithmetic diff", "project", "location", "cluster")

	ClusterStdEnd = newGauge("gke_cluster_standard_support_end_timestamp", "standard support EOL (unix sec)", "project", "location", "cluster")
	ClusterExtEnd = newGauge("gke_cluster_extended_support_end_timestamp", "extended support EOL (unix sec)", "project", "location", "cluster")

	NodepoolInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gke_nodepool_info",
		Help: "GKE nodepool version info.",
	}, []string{"project", "location", "cluster", "nodepool", "current_version", "max_upgradable_version", "latest_available_version"})

	NodepoolCurToMaxBehind    = newGauge("gke_nodepool_current_to_max_versions_behind", "", "project", "location", "cluster", "nodepool")
	NodepoolCurToMaxDiff      = newGauge("gke_nodepool_current_to_max_version_diff", "", "project", "location", "cluster", "nodepool")
	NodepoolMaxToLatestBehind = newGauge("gke_nodepool_max_to_latest_versions_behind", "", "project", "location", "cluster", "nodepool")
	NodepoolMaxToLatestDiff   = newGauge("gke_nodepool_max_to_latest_version_diff", "", "project", "location", "cluster", "nodepool")
	NodepoolCurToLatestBehind = newGauge("gke_nodepool_current_to_latest_versions_behind", "", "project", "location", "cluster", "nodepool")
	NodepoolCurToLatestDiff   = newGauge("gke_nodepool_current_to_latest_version_diff", "", "project", "location", "cluster", "nodepool")
	NodepoolStdEnd            = newGauge("gke_nodepool_standard_support_end_timestamp", "", "project", "location", "cluster", "nodepool")
	NodepoolExtEnd            = newGauge("gke_nodepool_extended_support_end_timestamp", "", "project", "location", "cluster", "nodepool")

	ExporterLastScrape    = newGauge("gke_exporter_last_scrape_timestamp", "", "cluster")
	ExporterScrapeErrors  = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gke_exporter_scrape_errors_total"}, []string{"cluster"})
	ExporterScrapeSeconds = newGauge("gke_exporter_scrape_duration_seconds", "", "cluster")
)

func newGauge(name, help string, labels ...string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
}

func MustRegister() {
	prometheus.MustRegister(
		ClusterInfo,
		ClusterCurToMaxBehind, ClusterCurToMaxDiff,
		ClusterMaxToLatestBehind, ClusterMaxToLatestDiff,
		ClusterCurToLatestBehind, ClusterCurToLatestDiff,
		ClusterStdEnd, ClusterExtEnd,
		NodepoolInfo,
		NodepoolCurToMaxBehind, NodepoolCurToMaxDiff,
		NodepoolMaxToLatestBehind, NodepoolMaxToLatestDiff,
		NodepoolCurToLatestBehind, NodepoolCurToLatestDiff,
		NodepoolStdEnd, NodepoolExtEnd,
		ExporterLastScrape, ExporterScrapeErrors, ExporterScrapeSeconds,
	)
}
