package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// adjustAnalysisHandler 站点调整分析：用户流失、满桩投诉、调拨成本。
func adjustAnalysisHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var name string
	var cap int
	if err := db.QueryRow(`SELECT name, capacity FROM stations WHERE id=$1`, id).Scan(&name, &cap); err != nil {
		writeErr(w, 404, "站点不存在")
		return
	}
	out := map[string]any{"station_id": id, "station_name": name, "capacity": cap}

	// 用户流失：最后一骑在本站且 ≥30 天未骑行
	var churn int
	db.QueryRow(`
		SELECT count(*) FROM (
			SELECT user_id, max(borrow_time) AS last_ride,
			       (SELECT borrow_station_id FROM rides r2
			         WHERE r2.user_id=r.user_id ORDER BY borrow_time DESC LIMIT 1) AS last_station
			FROM rides r GROUP BY user_id
		) t WHERE t.last_station=$1 AND t.last_ride < now()-interval '30 days'`, id).Scan(&churn)
	out["churned_users_30d"] = churn

	// 满桩投诉（近 30 天）
	var fullComplaints, emptyComplaints int
	db.QueryRow(`SELECT count(*) FROM complaints WHERE station_id=$1 AND type='full' AND created_at>now()-interval '30 days'`, id).Scan(&fullComplaints)
	db.QueryRow(`SELECT count(*) FROM complaints WHERE station_id=$1 AND type='empty' AND created_at>now()-interval '30 days'`, id).Scan(&emptyComplaints)
	out["full_complaints_30d"] = fullComplaints
	out["empty_complaints_30d"] = emptyComplaints

	// 调拨成本（近 30 天，本站作为调出或调入）
	var cost sql.NullFloat64
	var taskCount int
	db.QueryRow(`SELECT COALESCE(sum(cost),0), count(*) FROM rebalance_tasks
		WHERE (from_station_id=$1 OR to_station_id=$1) AND created_at>now()-interval '30 days'`,
		id).Scan(&cost, &taskCount)
	out["rebalance_cost_30d"] = cost.Float64
	out["rebalance_tasks_30d"] = taskCount

	// 近 30 天骑行量
	var rideCount int
	db.QueryRow(`SELECT count(*) FROM rides WHERE (borrow_station_id=$1 OR return_station_id=$1)
		AND borrow_time>now()-interval '30 days'`, id).Scan(&rideCount)
	out["rides_30d"] = rideCount

	// 调整历史
	adj := []map[string]any{}
	rows, err := db.Query(`SELECT a.action, a.detail, COALESCE(u.name,''), a.created_at
		FROM station_adjustments a LEFT JOIN users u ON u.id=a.operator_id
		WHERE a.station_id=$1 ORDER BY a.id DESC`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				action, detail, op string
				at                 time.Time
			)
			if rows.Scan(&action, &detail, &op, &at) == nil {
				adj = append(adj, map[string]any{
					"action": action, "detail": detail, "operator": op, "created_at": at,
				})
			}
		}
	}
	out["adjustments"] = adj
	writeJSON(w, 200, out)
}

// stationAdjustHandler 记录站点调整（容量变更等）。
func stationAdjustHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Action     string `json:"action"` // capacity_expand / capacity_shrink / relocate / other
		Detail     string `json:"detail"`
		NewCapacity int   `json:"new_capacity"`
	}
	if err := decodeBody(r, &req); err != nil || req.Action == "" {
		writeErr(w, 400, "参数不完整")
		return
	}
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	if req.NewCapacity > 0 {
		var oldCap int
		if err := tx.QueryRow(`SELECT capacity FROM stations WHERE id=$1`, id).Scan(&oldCap); err != nil {
			writeErr(w, 404, "站点不存在")
			return
		}
		if req.NewCapacity != oldCap {
			if _, err := tx.Exec(`UPDATE stations SET capacity=$1 WHERE id=$2`, req.NewCapacity, id); err != nil {
				writeErr(w, 500, "更新容量失败")
				return
			}
			// 同步桩位数量
			var dockCount int
			tx.QueryRow(`SELECT count(*) FROM docks WHERE station_id=$1`, id).Scan(&dockCount)
			for i := dockCount + 1; i <= req.NewCapacity; i++ {
				tx.Exec(`INSERT INTO docks(station_id,dock_no,status) VALUES($1,$2,'empty') ON CONFLICT DO NOTHING`, id, i)
			}
			req.Detail = fmt.Sprintf("桩位 %d → %d。%s", oldCap, req.NewCapacity, req.Detail)
		}
	}
	if _, err := tx.Exec(`INSERT INTO station_adjustments(station_id,action,detail,operator_id)
		VALUES($1,$2,$3,$4)`, id, req.Action, req.Detail, u.ID); err != nil {
		writeErr(w, 500, "记录失败")
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "站点调整已记录"})
}

// weatherHandler 恶劣天气停运/恢复。
func weatherHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Active  bool   `json:"active"`
		Level   string `json:"level"`
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	if req.Active {
		if req.Level == "" {
			req.Level = "橙色"
		}
		if req.Content == "" {
			req.Content = "恶劣天气预警：全市站点暂停借车，还车正常"
		}
		tx.Exec(`UPDATE weather_alerts SET active=false`)
		if _, err := tx.Exec(`INSERT INTO weather_alerts(level,content,active) VALUES($1,$2,true)`,
			req.Level, req.Content); err != nil {
			writeErr(w, 500, "发布预警失败")
			return
		}
		tx.Exec(`UPDATE stations SET status='weather_suspended' WHERE status='normal'`)
	} else {
		tx.Exec(`UPDATE weather_alerts SET active=false`)
		tx.Exec(`UPDATE stations SET status='normal' WHERE status='weather_suspended'`)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	msg := "已发布恶劣天气停运：全部站点暂停借车"
	if !req.Active {
		msg = "恶劣天气预警已解除，站点恢复运营"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func forbiddenZonesHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`SELECT id, name, description, active FROM forbidden_zones ORDER BY id`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id          int64
			name, desc  string
			active      bool
		)
		if rows.Scan(&id, &name, &desc, &active) == nil {
			out = append(out, map[string]any{"id": id, "name": name, "description": desc, "active": active})
		}
	}
	writeJSON(w, 200, out)
}

func forbiddenZoneToggleHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	db.Exec(`UPDATE forbidden_zones SET active=NOT active WHERE id=$1`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func cleaningHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT c.id, b.code, COALESCE(s.name,''), c.scheduled_date, c.cleaner, c.status
		FROM cleaning_schedules c
		JOIN bikes b ON b.id=c.bike_id
		LEFT JOIN stations s ON s.id=c.station_id
		ORDER BY c.id DESC LIMIT 50`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                int64
			code, station     string
			date              time.Time
			cleaner, status   string
		)
		if rows.Scan(&id, &code, &station, &date, &cleaner, &status) == nil {
			out = append(out, map[string]any{
				"id": id, "bike_code": code, "station": station,
				"date": date.Format("2006-01-02"), "cleaner": cleaner, "status": status,
			})
		}
	}
	writeJSON(w, 200, out)
}

func cleaningDoneHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	res, err := db.Exec(`UPDATE cleaning_schedules SET status='done' WHERE id=$1 AND status='pending'`, id)
	if err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "任务不存在或已完成")
		return
	}
	db.Exec(`UPDATE bikes SET last_cleaned_at=now(), status=CASE WHEN status='cleaning' THEN 'docked' ELSE status END
		WHERE id=(SELECT bike_id FROM cleaning_schedules WHERE id=$1)`, id)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "清洗完成"})
}

func shiftsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT s.id, u.name, s.shift_date, s.start_time, s.end_time, COALESCE(t.plate,''), s.status
		FROM driver_shifts s
		JOIN users u ON u.id=s.driver_id
		LEFT JOIN trucks t ON t.id=s.truck_id
		ORDER BY s.shift_date DESC, s.start_time LIMIT 50`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                          int64
			driver, start, end, plate   string
			status                      string
			date                        time.Time
		)
		if rows.Scan(&id, &driver, &date, &start, &end, &plate, &status) == nil {
			out = append(out, map[string]any{
				"id": id, "driver": driver, "date": date.Format("2006-01-02"),
				"start": start, "end": end, "truck": plate, "status": status,
			})
		}
	}
	writeJSON(w, 200, out)
}

func createShiftHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	var req struct {
		DriverID int64  `json:"driver_id"`
		Date     string `json:"date"`
		Start    string `json:"start"`
		End      string `json:"end"`
		TruckID  *int64 `json:"truck_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.DriverID == 0 || req.Date == "" {
		writeErr(w, 400, "参数不完整")
		return
	}
	if _, err := db.Exec(`INSERT INTO driver_shifts(driver_id,shift_date,start_time,end_time,truck_id)
		VALUES($1,$2,$3,$4,$5)`, req.DriverID, req.Date, req.Start, req.End, req.TruckID); err != nil {
		writeErr(w, 500, "排班失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "排班已创建"})
}

func stationPowerHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var req struct {
		Status string `json:"status"` // normal / outage
	}
	if err := decodeBody(r, &req); err != nil || (req.Status != "normal" && req.Status != "outage") {
		writeErr(w, 400, "参数错误")
		return
	}
	res, err := db.Exec(`UPDATE stations SET power_status=$1 WHERE id=$2`, req.Status, id)
	if err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 404, "站点不存在")
		return
	}
	msg := "站点电源已恢复"
	if req.Status == "outage" {
		msg = "已标记电源故障：该站暂停借车（智能锁无电）"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

// opsOverviewHandler 运营判断面板：班次/电源/清洗/禁区/天气/突发客流汇总。
func opsOverviewHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	out := map[string]any{}
	var activeShifts, cleaningPending, activeZones, surgeCount int
	db.QueryRow(`SELECT count(*) FROM driver_shifts WHERE shift_date=CURRENT_DATE AND status='active'`).Scan(&activeShifts)
	db.QueryRow(`SELECT count(*) FROM cleaning_schedules WHERE status='pending'`).Scan(&cleaningPending)
	db.QueryRow(`SELECT count(*) FROM forbidden_zones WHERE active`).Scan(&activeZones)
	db.QueryRow(`SELECT count(*) FROM subway_flows WHERE flow_date=CURRENT_DATE AND surge`).Scan(&surgeCount)
	out["active_shifts"] = activeShifts
	out["cleaning_pending"] = cleaningPending
	out["active_zones"] = activeZones
	out["surge_alerts"] = surgeCount
	var weatherActive bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM weather_alerts WHERE active)`).Scan(&weatherActive)
	out["weather_suspended"] = weatherActive
	var powerOutage int
	db.QueryRow(`SELECT count(*) FROM stations WHERE power_status<>'normal'`).Scan(&powerOutage)
	out["power_outage_stations"] = powerOutage
	writeJSON(w, 200, out)
}

func usersHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	role := r.URL.Query().Get("role")
	q := `SELECT id, username, name, role FROM users`
	args := []any{}
	if role != "" {
		q += ` WHERE role=$1`
		args = append(args, role)
	}
	q += ` ORDER BY id`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id               int64
			username, name   string
			rl               string
		)
		if rows.Scan(&id, &username, &name, &rl) == nil {
			out = append(out, map[string]any{
				"id": id, "username": username, "name": name, "role": rl, "role_name": roleNames[rl],
			})
		}
	}
	writeJSON(w, 200, out)
}
