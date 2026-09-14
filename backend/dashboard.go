package main

import (
	"database/sql"
	"net/http"
)

// dashboardHandler 运营实时总览：满桩/空桩/故障/调拨车/高峰需求/地铁客流/事件。
func dashboardHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	out := map[string]any{}

	stations, err := stationRows()
	if err != nil {
		writeErr(w, 500, "查询站点失败")
		return
	}
	full, empty, faultTotal := 0, 0, 0
	for _, s := range stations {
		switch s["state"] {
		case "full":
			full++
		case "empty":
			empty++
		}
		faultTotal += s["fault_bikes"].(int)
	}
	out["stations"] = stations
	out["full_stations"] = full
	out["empty_stations"] = empty
	out["fault_bikes"] = faultTotal

	// 调拨车
	trucks := []map[string]any{}
	trows, err := db.Query(`SELECT t.id, t.plate, t.capacity, t.status, t.location_x, t.location_y,
		COALESCE(u.name,''), COALESCE(t.current_task_id,0)
		FROM trucks t LEFT JOIN users u ON u.id=t.driver_id ORDER BY t.id`)
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var (
				id, taskID int64
				plate, st, drv string
				cap        int
				x, y       float64
			)
			if trows.Scan(&id, &plate, &cap, &st, &x, &y, &drv, &taskID) == nil {
				trucks = append(trucks, map[string]any{
					"id": id, "plate": plate, "capacity": cap, "status": st,
					"x": x, "y": y, "driver": drv, "task_id": taskID,
				})
			}
		}
	}
	out["trucks"] = trucks

	// 进行中行程 / 待处理故障 / 打开事件
	var ongoing, pendingFaults, openEvents, pendingAppeals int
	db.QueryRow(`SELECT count(*) FROM rides WHERE status='ongoing'`).Scan(&ongoing)
	db.QueryRow(`SELECT count(*) FROM faults WHERE status IN ('pending','assigned','repairing')`).Scan(&pendingFaults)
	db.QueryRow(`SELECT count(*) FROM events WHERE status<>'resolved'`).Scan(&openEvents)
	db.QueryRow(`SELECT count(*) FROM appeals WHERE status IN ('pending','processing')`).Scan(&pendingAppeals)
	out["ongoing_rides"] = ongoing
	out["pending_faults"] = pendingFaults
	out["open_events"] = openEvents
	out["pending_appeals"] = pendingAppeals

	// 今日分时段骑行量（早晚高峰需求）
	hourly := make([]map[string]any, 24)
	for h := 0; h < 24; h++ {
		hourly[h] = map[string]any{"hour": h, "borrows": 0, "returns": 0}
	}
	hrows, err := db.Query(`
		SELECT extract(hour FROM borrow_time)::int AS h, count(*), 0 FROM rides
		WHERE borrow_time::date=CURRENT_DATE GROUP BY 1
		UNION ALL
		SELECT extract(hour FROM return_time)::int, 0, count(*) FROM rides
		WHERE return_time IS NOT NULL AND return_time::date=CURRENT_DATE GROUP BY 1`)
	if err == nil {
		defer hrows.Close()
		for hrows.Next() {
			var h, b, rt int
			if hrows.Scan(&h, &b, &rt) == nil && h >= 0 && h < 24 {
				hourly[h]["borrows"] = hourly[h]["borrows"].(int) + b
				hourly[h]["returns"] = hourly[h]["returns"].(int) + rt
			}
		}
	}
	// 叠加需求预测（未来 3 小时，按 Asia/Shanghai 时段）
	hour := hourCN()
	forecast := []map[string]any{}
	frows, err := db.Query(`SELECT hour, sum(borrow_need), sum(return_need) FROM demand_profiles
		WHERE hour BETWEEN $1 AND $2 GROUP BY hour ORDER BY hour`, hour, hour+3)
	if err == nil {
		defer frows.Close()
		for frows.Next() {
			var h, b, rt int
			if frows.Scan(&h, &b, &rt) == nil {
				forecast = append(forecast, map[string]any{"hour": h, "borrow_need": b, "return_need": rt})
			}
		}
	}
	out["hourly"] = hourly
	out["forecast"] = forecast

	// 地铁口客流（今日）
	flows := []map[string]any{}
	flRows, err := db.Query(`SELECT s.name, f.hour, f.flow, f.surge FROM subway_flows f
		JOIN stations s ON s.id=f.station_id WHERE f.flow_date=CURRENT_DATE ORDER BY s.id, f.hour`)
	if err == nil {
		defer flRows.Close()
		for flRows.Next() {
			var (
				name  string
				h, fl int
				surge bool
			)
			if flRows.Scan(&name, &h, &fl, &surge) == nil {
				flows = append(flows, map[string]any{"station": name, "hour": h, "flow": fl, "surge": surge})
			}
		}
	}
	out["subway_flows"] = flows

	// 天气 / 预警
	var weatherActive bool
	var weatherContent sql.NullString
	db.QueryRow(`SELECT true, content FROM weather_alerts WHERE active ORDER BY id DESC LIMIT 1`).
		Scan(&weatherActive, &weatherContent)
	out["weather_alert"] = map[string]any{"active": weatherActive, "content": weatherContent.String}

	writeJSON(w, 200, out)
}
