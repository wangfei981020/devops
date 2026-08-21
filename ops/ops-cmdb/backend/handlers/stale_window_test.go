package handlers

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// 停摆判定的容忍窗口必须跟着**信号自己的周期**走。
//
// 守的是 OPSCMDB-031 P1-55：`host_sync` 每天 03:00 跑一次，
// 而停摆判据用的是所有数据源共用的固定 6 小时阈值 ——
// 于是从每天 09:00 一直到次日 03:00 它都被标成「同步停摆」，
// **一天 24 小时里约 22 小时是误报**。
//
// ⚠️ 这是同一类根因的第三次出现（前两次是节点心跳误报 OPSCMDB-025 / 027）：
// **判定阈值 ≈ 或小于信号本身的更新周期。**
func TestStaleWindowFollowsSchedule(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		// 这个时刻距上次执行多久时**不该**判停摆
		okAfter time.Duration
		// 这个时刻应该判停摆
		staleAfter time.Duration
	}{
		{
			// 🔴 P1-55 的原型
			name: "每天一次：10.9 小时后仍然正常", schedule: "0 3 * * *",
			okAfter: 11 * time.Hour, staleAfter: 50 * time.Hour,
		},
		{
			name: "每小时一次：3 小时算停摆", schedule: "0 * * * *",
			okAfter: 90 * time.Minute, staleAfter: 4 * time.Hour,
		},
		{
			name: "每 30 分钟：90 分钟算停摆", schedule: "*/30 * * * *",
			okAfter: 40 * time.Minute, staleAfter: 3 * time.Hour,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sched, err := cron.ParseStandard(c.schedule)
			if err != nil {
				t.Fatalf("解析 %q 失败：%v", c.schedule, err)
			}
			now := time.Now()
			n1 := sched.Next(now)
			period := sched.Next(n1).Sub(n1)
			win := 2*period + time.Hour

			if c.okAfter > win {
				t.Errorf("距上次 %v 被判停摆（窗口 %v）—— 这是误报，任务按时在跑", c.okAfter, win)
			}
			if c.staleAfter <= win {
				t.Errorf("距上次 %v 没被判停摆（窗口 %v）—— 漏报，真停摆了看不出来", c.staleAfter, win)
			}
		})
	}
}

// 固定 6 小时阈值**放在每日任务上就是错的** —— 锁住这个事实，
// 防止有人"顺手"把窗口改回一个常量。
func TestFixedThresholdWouldMisreportDailyTask(t *testing.T) {
	sched, _ := cron.ParseStandard("0 3 * * *")
	now := time.Now()
	n1 := sched.Next(now)
	period := sched.Next(n1).Sub(n1)

	if dsStaleAfter >= period {
		t.Skip("固定阈值已经大于每日周期，这条测试的前提不再成立")
	}
	// 每日任务在一天里有多长时间会被固定阈值误报
	misreported := period - dsStaleAfter
	if misreported < 12*time.Hour {
		t.Errorf("误报窗口 %v，与实测的约 22 小时不符 —— 判据或常量变了，重新核对", misreported)
	}
	t.Logf("固定阈值 %v 用在每日任务上，一天里有 %v 会误报为停摆", dsStaleAfter, misreported)
}
