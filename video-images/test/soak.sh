#!/bin/sh
# 软测采集脚本:定时快照生成端各 pod 的 内存/ffmpeg数/僵尸/线程 + MinIO 对象数,输出 CSV。
# 用来在真集群跑 24-48h,看内存/线程是否随时间漂移(泄漏)、有无僵尸堆积。
#
# 用法:  ./soak.sh [app_namespace] [minio_namespace] [间隔秒] [次数]
# 例:    ./soak.sh video-images devops-minio 300 288   # 每5分钟一次,共24小时
#
# 输出:  soak-YYYYmmdd.csv(可直接导入表格画趋势图)

NS=${1:-video-images}
MINIO_NS=${2:-devops-minio}
INTERVAL=${3:-300}
COUNT=${4:-288}
OUT="soak-$(date +%Y%m%d-%H%M%S).csv"

echo "ts,pod,cgroup_mem_mb,ffmpeg,zombie,threads,restarts" > "$OUT"
echo "采集中 -> $OUT (每 ${INTERVAL}s, 共 ${COUNT} 次)"

i=0
while [ "$i" -lt "$COUNT" ]; do
  now=$(date +%Y-%m-%dT%H:%M:%S)
  for pod in $(kubectl get pods -n "$NS" -l app=video-images-generator -o name 2>/dev/null | sed 's|pod/||'); do
    line=$(kubectl exec -n "$NS" "$pod" -- sh -c '
      mem=$(cat /sys/fs/cgroup/memory.current 2>/dev/null | awk "{print int(\$1/1024/1024)}")
      ff=$(ls -d /proc/[0-9]* 2>/dev/null | while read d; do cat $d/comm 2>/dev/null; done | grep -c ffmpeg)
      z=$(for d in /proc/[0-9]*; do awk "{print \$3}" $d/stat 2>/dev/null; done | grep -c Z)
      th=$(awk "/Threads/{print \$2}" /proc/1/status 2>/dev/null)
      echo "$mem,$ff,$z,$th"
    ' 2>/dev/null)
    rs=$(kubectl get pod -n "$NS" "$pod" -o jsonpath='{.status.containerStatuses[0].restartCount}' 2>/dev/null)
    echo "$now,$pod,$line,$rs" | tee -a "$OUT"
  done
  obj=$(kubectl exec -n "$MINIO_NS" devops-minio-0 -- sh -c 'mc alias set m http://localhost:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null 2>&1; mc ls m/video-images 2>/dev/null | wc -l' 2>/dev/null)
  echo "$now,MINIO_OBJECTS,$obj,,,," | tee -a "$OUT"
  i=$((i + 1))
  [ "$i" -lt "$COUNT" ] && sleep "$INTERVAL"
done
echo "完成: $OUT"
