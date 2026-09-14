package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func rebalanceTasksHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT t.id, s1.name, s2.name, t.bike_count, COALESCE(k.plate,''), COALESCE(u.name,''),
		       t.status, t.reason, t.cost, t.created_at, t.completed_at
		FROM rebalance_tasks t
		JOIN stations s1 ON s1.id=t.from_station_id
		JOIN stations s2 ON s2.id=t.to_station_id
		LEFT JOIN trucks k ON k.id=t.truck_id
		LEFT JOIN users u ON u.id=t.driver_id
		ORDER BY t.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, cnt                    int64
			from, to, plate, driver    string
			status, reason             string
			cost                       float64
			created                    time.Time
			completed                  sql.NullTime
		)
		if err := rows.Scan(&id, &from, &to, &cnt, &plate, &driver, &status, &reason, &cost, &created, &completed); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "from_station": from, "to_station": to, "bike_count": cnt,
			"truck": plate, "driver": driver, "status": status, "reason": reason,
			"cost": cost, "created_at": created, "completed_at": timePtr(completed),
		})
	}
	writeJSON(w, 200, out)
}

func rebalanceTaskDetailHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var (
		from, to, status, reason, explanation, factors string
		cnt                                            int64
		plate, driver                                  sql.NullString
		cost                                           float64
		created                                        time.Time
		completed                                      sql.NullTime
	)
	err := db.QueryRow(`
		SELECT s1.name, s2.name, t.bike_count, t.status, t.reason, t.explanation, t.factors,
		       COALESCE(k.plate,''), COALESCE(u.name,''), t.cost, t.created_at, t.completed_at
		FROM rebalance_tasks t
		JOIN stations s1 ON s1.id=t.from_station_id
		JOIN stations s2 ON s2.id=t.to_station_id
		LEFT JOIN trucks k ON k.id=t.truck_id
		LEFT JOIN users u ON u.id=t.driver_id
		WHERE t.id=$1`, id).Scan(&from, &to, &cnt, &status, &reason, &explanation, &factors,
		&plate, &driver, &cost, &created, &completed)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "任务不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	var factorMap map[string]any
	json.Unmarshal([]byte(factors), &factorMap)
	writeJSON(w, 200, map[string]any{
		"id": id, "from_station": from, "to_station": to, "bike_count": cnt,
		"status": status, "reason": reason, "explanation": explanation, "factors": factorMap,
		"truck": plate.String, "driver": driver.String, "cost": cost,
		"created_at": created, "completed_at": timePtr(completed),
	})
}

type stationNeed struct {
	ID         int64
	Name       string
	Type       string
	Cap        int
	Avail      int
	Fault      int
	BorrowNeed int
	ReturnNeed int
	Surge      bool
	Expected   int // 未来 2 小时预计在桩
	Surplus    int
	Deficit    int
	Reasons    []string
}

// rebalancePlanHandler 调拨策略生成：地铁早高峰、学校放学、商圈活动、维修车比例、调拨车容量。
func rebalancePlanHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		SubwayPeak       bool    `json:"subway_peak"`
		SchoolDismissal  bool    `json:"school_dismissal"`
		BusinessEvent    bool    `json:"business_event"`
		RepairRatioLimit float64 `json:"repair_ratio_limit"` // 维修车比例阈值，如 0.15
		TruckCapacity    int     `json:"truck_capacity"`     // 0 = 按可用调拨车最小容量
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.RepairRatioLimit <= 0 {
		req.RepairRatioLimit = 0.15
	}
	if req.TruckCapacity <= 0 {
		db.QueryRow(`SELECT COALESCE(MIN(capacity),20) FROM trucks WHERE status='idle'`).Scan(&req.TruckCapacity)
	}
	hour := time.Now().Hour()

	rows, err := db.Query(`
		SELECT s.id, s.name, s.type, s.capacity,
		       (SELECT count(*) FROM bikes b WHERE b.station_id=s.id AND b.status='docked'),
		       (SELECT count(*) FROM bikes b WHERE b.station_id=s.id AND b.status='fault'),
		       COALESCE((SELECT borrow_need FROM demand_profiles p WHERE p.station_id=s.id AND p.hour=$1),0),
		       COALESCE((SELECT return_need FROM demand_profiles p WHERE p.station_id=s.id AND p.hour=$1),0),
		       EXISTS(SELECT 1 FROM subway_flows f WHERE f.station_id=s.id AND f.flow_date=CURRENT_DATE AND f.surge)
		FROM stations s WHERE s.status='normal' ORDER BY s.id`, hour)
	if err != nil {
		writeErr(w, 500, "读取站点需求失败")
		return
	}
	defer rows.Close()
	needs := []stationNeed{}
	for rows.Next() {
		var sn stationNeed
		if err := rows.Scan(&sn.ID, &sn.Name, &sn.Type, &sn.Cap, &sn.Avail, &sn.Fault,
			&sn.BorrowNeed, &sn.ReturnNeed, &sn.Surge); err != nil {
			continue
		}
		needs = append(needs, sn)
	}

	// ---- 策略因子修正 ----
	factorsUsed := map[string]any{
		"subway_peak": req.SubwayPeak, "school_dismissal": req.SchoolDismissal,
		"business_event": req.BusinessEvent, "repair_ratio_limit": req.RepairRatioLimit,
		"truck_capacity": req.TruckCapacity, "hour": hour,
	}
	for i := range needs {
		sn := &needs[i]
		if req.SubwayPeak && sn.Type == "subway" && hour >= 6 && hour <= 10 {
			sn.BorrowNeed += 10
			sn.Reasons = append(sn.Reasons, "地铁早高峰借车需求上调")
		}
		if sn.Surge {
			sn.BorrowNeed += 12
			sn.Reasons = append(sn.Reasons, "地铁突发客流预警")
		}
		if req.SchoolDismissal && sn.Type == "school" && hour >= 14 && hour <= 18 {
			sn.BorrowNeed += 8
			sn.Reasons = append(sn.Reasons, "学校放学时段借车上调")
		}
		if req.BusinessEvent && sn.Type == "business" && hour >= 17 && hour <= 22 {
			sn.ReturnNeed += 10
			sn.Reasons = append(sn.Reasons, "商圈活动还车集中")
		}
		sn.Expected = sn.Avail + sn.ReturnNeed - sn.BorrowNeed
		// 维修车比例
		total := sn.Avail + sn.Fault
		if total > 0 && float64(sn.Fault)/float64(total) >= req.RepairRatioLimit && sn.Fault >= 2 {
			sn.Reasons = append(sn.Reasons, fmt.Sprintf("故障车占比 %.0f%% 超阈值", float64(sn.Fault)/float64(total)*100))
		}
		// 盈余 / 缺口
		if sn.Expected > int(float64(sn.Cap)*0.85) {
			sn.Surplus = sn.Expected - int(float64(sn.Cap)*0.7)
			sn.Reasons = append(sn.Reasons, fmt.Sprintf("预计满桩率 %.0f%%", float64(sn.Expected)/float64(sn.Cap)*100))
		}
		if sn.Expected < int(float64(sn.Cap)*0.15) {
			sn.Deficit = int(float64(sn.Cap)*0.3) - sn.Expected
			if sn.Deficit < 3 {
				sn.Deficit = 3
			}
			sn.Reasons = append(sn.Reasons, fmt.Sprintf("预计可用车不足（%d 辆）", sn.Expected))
		}
	}

	// ---- 盈余站 → 缺口站 贪心匹配（受调拨车容量约束）----
	created := []map[string]any{}
	for di := range needs {
		d := &needs[di]
		for d.Deficit > 0 {
			// 找盈余最多的站
			best := -1
			for si := range needs {
				if needs[si].Surplus > 0 && (best == -1 || needs[si].Surplus > needs[best].Surplus) {
					best = si
				}
			}
			if best == -1 {
				break
			}
			s := &needs[best]
			cnt := min3(s.Surplus, d.Deficit, req.TruckCapacity)
			if cnt <= 0 {
				break
			}
			s.Surplus -= cnt
			d.Deficit -= cnt
			reason := "供需平衡调拨"
			explanation := fmt.Sprintf("【%s → %s】调出站：%s（在桩 %d/%d，%s）；调入站：%s（可用 %d 辆，%s）。调拨 %d 辆（调拨车容量 %d）。",
				s.Name, d.Name, s.Name, s.Avail, s.Cap, joinReasons(s.Reasons),
				d.Name, d.Avail, joinReasons(d.Reasons), cnt, req.TruckCapacity)
			cost := float64(cnt)*2.5 + 12.0
			var taskID int64
			fj, _ := json.Marshal(factorsUsed)
			err := db.QueryRow(`INSERT INTO rebalance_tasks(from_station_id,to_station_id,bike_count,status,reason,explanation,factors,cost,created_by)
				VALUES($1,$2,$3,'pending',$4,$5,$6,$7,$8) RETURNING id`,
				s.ID, d.ID, cnt, reason, explanation, string(fj), cost, u.ID).Scan(&taskID)
			if err != nil {
				writeErr(w, 500, "生成任务失败")
				return
			}
			created = append(created, map[string]any{
				"id": taskID, "from": s.Name, "to": d.Name, "count": cnt,
				"explanation": explanation, "cost": cost,
			})
		}
	}
	if len(created) == 0 {
		writeJSON(w, 200, map[string]any{
			"created": created, "factors": factorsUsed,
			"message": "当前各站点供需在阈值内，无需调拨",
		})
		return
	}
	writeJSON(w, 200, map[string]any{
		"created": created, "factors": factorsUsed,
		"message": fmt.Sprintf("已生成 %d 个调拨任务，等待派车", len(created)),
	})
}

func joinReasons(rs []string) string {
	if len(rs) == 0 {
		return "常规周转"
	}
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += "；"
		}
		out += r
	}
	return out
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func rebalanceAssignHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		TruckID int64 `json:"truck_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.TruckID == 0 {
		writeErr(w, 400, "请选择调拨车")
		return
	}
	var driverID sql.NullInt64
	var cap, status string
	err := db.QueryRow(`SELECT driver_id, capacity, status FROM trucks WHERE id=$1`, req.TruckID).
		Scan(&driverID, &cap, &status)
	if err != nil {
		writeErr(w, 404, "调拨车不存在")
		return
	}
	if status != "idle" {
		writeErr(w, 409, "该调拨车正在执行任务")
		return
	}
	res, err := db.Exec(`UPDATE rebalance_tasks SET truck_id=$1, driver_id=$2, status='assigned'
		WHERE id=$3 AND status='pending'`, req.TruckID, driverID, id)
	if err != nil {
		writeErr(w, 500, "派车失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "任务状态不允许派车")
		return
	}
	db.Exec(`UPDATE trucks SET status='loading', current_task_id=$1 WHERE id=$2`, id, req.TruckID)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已派车，等待司机出车"})
}

// rebalanceStatusHandler: start / complete / stuck
func rebalanceStatusHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	var (
		fromID, toID, cnt int64
		truckID           sql.NullInt64
		curStatus         string
	)
	err := db.QueryRow(`SELECT from_station_id, to_station_id, bike_count, truck_id, status
		FROM rebalance_tasks WHERE id=$1`, id).Scan(&fromID, &toID, &cnt, &truckID, &curStatus)
	if err != nil {
		writeErr(w, 404, "任务不存在")
		return
	}
	switch req.Status {
	case "in_progress":
		if curStatus != "assigned" {
			writeErr(w, 409, "任务未派车")
			return
		}
		db.Exec(`UPDATE rebalance_tasks SET status='in_progress' WHERE id=$1`, id)
		if truckID.Valid {
			db.Exec(`UPDATE trucks SET status='en_route' WHERE id=$1`, truckID.Int64)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": "调拨车已出车"})
	case "stuck":
		if curStatus != "in_progress" {
			writeErr(w, 409, "任务未在途中")
			return
		}
		db.Exec(`UPDATE rebalance_tasks SET status='stuck' WHERE id=$1`, id)
		if truckID.Valid {
			db.Exec(`UPDATE trucks SET status='stuck' WHERE id=$1`, truckID.Int64)
		}
		// 自动建立「调拨车堵在路上」协同事件
		var fromName string
		db.QueryRow(`SELECT name FROM stations WHERE id=$1`, fromID).Scan(&fromName)
		taskID := int64(0)
		fmt.Sscan(id, &taskID)
		createEvent("truck_stuck", fmt.Sprintf("调拨车堵在路上（任务 #%s，%s 方向）", id, fromName),
			fromID, nil, nil, &taskID, u.ID, "high")
		writeJSON(w, 200, map[string]any{"ok": true, "message": "已标记拥堵，并创建协同事件"})
	case "complete":
		if curStatus != "in_progress" && curStatus != "stuck" {
			writeErr(w, 409, "任务未在途中")
			return
		}
		tx, ok := mustTx(w)
		if !ok {
			return
		}
		defer tx.Rollback()
		// 实际搬移车辆：从调出站取 N 辆在桩车 → 调入站空桩
		moved := 0
		bikeRows, err := tx.Query(`SELECT id FROM bikes
			WHERE station_id=$1 AND status='docked' ORDER BY id LIMIT $2`, fromID, cnt)
		if err != nil {
			writeErr(w, 500, "读取车辆失败")
			return
		}
		bikeIDs := []int64{}
		for bikeRows.Next() {
			var b int64
			if bikeRows.Scan(&b) == nil {
				bikeIDs = append(bikeIDs, b)
			}
		}
		bikeRows.Close()
		for _, b := range bikeIDs {
			var dockID int64
			err := tx.QueryRow(`UPDATE docks SET status='occupied', bike_id=$1
				WHERE id=(SELECT id FROM docks WHERE station_id=$2 AND status='empty' ORDER BY dock_no LIMIT 1 FOR UPDATE)
				RETURNING id`, b, toID).Scan(&dockID)
			if err != nil {
				continue // 调入站满，跳过
			}
			tx.Exec(`UPDATE docks SET status='empty', bike_id=NULL WHERE station_id=$1 AND bike_id=$2`, fromID, b)
			tx.Exec(`UPDATE bikes SET station_id=$1 WHERE id=$2`, toID, b)
			moved++
		}
		if _, err := tx.Exec(`UPDATE rebalance_tasks SET status='completed', completed_at=now(), bike_count=$1 WHERE id=$2`, moved, id); err != nil {
			writeErr(w, 500, "更新任务失败")
			return
		}
		if truckID.Valid {
			tx.Exec(`UPDATE trucks SET status='idle', current_task_id=NULL WHERE id=$1`, truckID.Int64)
		}
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, "提交失败")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": fmt.Sprintf("调拨完成，实际搬移 %d 辆", moved)})
	default:
		writeErr(w, 400, "未知状态")
	}
}

func trucksHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`SELECT t.id, t.plate, t.capacity, t.status, COALESCE(u.name,''), COALESCE(t.current_task_id,0)
		FROM trucks t LEFT JOIN users u ON u.id=t.driver_id ORDER BY t.id`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, taskID int64
			plate      string
			cap        int
			status     string
			driver     string
		)
		if err := rows.Scan(&id, &plate, &cap, &status, &driver, &taskID); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "plate": plate, "capacity": cap, "status": status,
			"driver": driver, "task_id": taskID,
		})
	}
	writeJSON(w, 200, out)
}
