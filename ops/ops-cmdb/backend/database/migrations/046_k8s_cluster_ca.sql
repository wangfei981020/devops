-- GKE 经 SA key 自动发现纳管：存集群 CA(base64)，连接时用 SA 换 OAuth token + 此 CA。
ALTER TABLE k8s_clusters ADD COLUMN ca_data MEDIUMTEXT AFTER endpoint;
