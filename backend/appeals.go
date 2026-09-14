package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"
)

func createAppealHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		RideID int64  `json:"ride_id"`
		Reason string `json:"reason"`
	}
	if err := decodeBody(r, &req); err != nil || req.RideID == 0 || req.Reason == "" {
		writeErr(w, 400, "请选择行程并填写申诉原因")
		return
	}
	var owner int64
	err := db.QueryRow(`SELECT user_id FROM rides WHERE id=$1`, req.RideID).Scan(&owner)
	if err != nil {
		writeErr(w, 404, "行程不存在")
		return
	}
	if owner != u.ID {
		writeErr(w, 403, "只能对自己的行程申诉")
		return
	}
	var exists bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM appeals WHERE ride_id=$1 AND status IN ('pending','processing'))`,
		req.RideID).Scan(&exists)
	if exists {
		writeErr(w, 409, "该行程已有处理中的申诉")
		return
	}
	var id int64
	err = db.QueryRow(`INSERT INTO appeals(ride_id,user_id,reason) VALUES($1,$2,$3) RETURNING id`,
		req.RideID, u.ID, req.Reason).Scan(&id)
	if err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	writeJSON(w, 200, map[string]any{"appeal_id": id, "message": "申诉已提交，客服将尽快处理"})
}

func appealsHandler(w http.ResponseWriter, r *http.Request, u *User) {
	q := `
		SELECT a.id, a.ride_id, u.name, a.reason, a.status, COALESCE(h.name,''),
		       a.refund, a.created_at, a.resolved_at
		FROM appeals a
		JOIN users u ON u.id=a.user_id
		LEFT JOIN users h ON h.id=a.handler_id`
	args := []any{}
	if u.Role == "user" {
		q += ` WHERE a.user_id=$1`
		args = append(args, u.ID)
	}
	q += ` ORDER BY a.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, rideID             int64
			name, reason, status   string
			handler                string
			refund                 float64
			created                time.Time
			resolved               sql.NullTime
		)
		if err := rows.Scan(&id, &rideID, &name, &reason, &status, &handler, &refund, &created, &resolved); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "ride_id": rideID, "user": name, "reason": reason, "status": status,
			"handler": handler, "refund": refund, "created_at": created, "resolved_at": timePtr(resolved),
		})
	}
	writeJSON(w, 200, out)
}

// appealDetailHandler 申诉溯源：从一次骑行回到借车桩位、还车状态、客服处理、调拨影响。
func appealDetailHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var (
		rideID, userID                int64
		userName, reason, status      string
		handler, resolution           string
		respFee, respBike, respSt     string
		refund                        float64
		created                       time.Time
		resolved                      sql.NullTime
	)
	err := db.QueryRow(`
		SELECT a.ride_id, a.user_id, u.name, a.reason, a.status, COALESCE(h.name,''),
		       a.resolution, a.resp_fee, a.resp_bike, a.resp_station, a.refund, a.created_at, a.resolved_at
		FROM appeals a JOIN users u ON u.id=a.user_id LEFT JOIN users h ON h.id=a.handler_id
		WHERE a.id=$1`, id).Scan(&rideID, &userID, &userName, &reason, &status, &handler,
		&resolution, &respFee, &respBike, &respSt, &refund, &created, &resolved)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "申诉不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if u.Role == "user" && userID != u.ID {
		writeErr(w, 403, "无权查看他人申诉")
		return
	}

	// ---- 骑行链路 ----
	var (
		bikeCode, rideStatus, loc, ft, ff string
		fromName, toName                  string
		fromDock, toDock                  int64
		fromSID, toSID                    sql.NullInt64
		bt                                time.Time
		rt                                sql.NullTime
		fee                               sql.NullFloat64
	)
	err = db.QueryRow(`
		SELECT b.code, r.status, r.user_location, r.fault_type, r.fault_feedback,
		       s1.name, COALESCE(s2.name,''), COALESCE(d1.dock_no,0), COALESCE(d2.dock_no,0),
		       r.borrow_station_id, r.return_station_id, r.borrow_time, r.return_time, r.fee
		FROM rides r
		JOIN bikes b ON b.id=r.bike_id
		JOIN stations s1 ON s1.id=r.borrow_station_id
		LEFT JOIN stations s2 ON s2.id=r.return_station_id
		LEFT JOIN docks d1 ON d1.id=r.borrow_dock_id
		LEFT JOIN docks d2 ON d2.id=r.return_dock_id
		WHERE r.id=$1`, rideID).Scan(&bikeCode, &rideStatus, &loc, &ft, &ff,
		&fromName, &toName, &fromDock, &toDock, &fromSID, &toSID, &bt, &rt, &fee)
	if err != nil {
		writeErr(w, 500, "查询行程失败")
		return
	}
	durationMin := 0.0
	if rt.Valid {
		durationMin = rt.Time.Sub(bt).Minutes()
	}

	// ---- 客服处理（该行程关联事件的协同记录）----
	csTrace := []map[string]any{}
	evRows, err := db.Query(`
		SELECT e.id, e.type, e.title, e.status, e.resolution, e.created_at
		FROM events e WHERE e.ride_id=$1 ORDER BY e.id`, rideID)
	if err == nil {
		for evRows.Next() {
			var (
				eid                     int64
				typ, title, est, res    string
				ec                      time.Time
			)
			if evRows.Scan(&eid, &typ, &title, &est, &res, &ec) == nil {
				csTrace = append(csTrace, map[string]any{
					"event_id": eid, "type": typ, "type_name": eventTypeNames[typ],
					"title": title, "status": est, "resolution": res, "created_at": ec,
				})
			}
		}
		evRows.Close()
	}

	// ---- 调拨影响（骑行前后 6 小时内涉及借/还车站点的调拨任务）----
	rebalanceImpact := []map[string]any{}
	rbRows, err := db.Query(`
		SELECT t.id, s1.name, s2.name, t.bike_count, t.status, t.reason, t.created_at
		FROM rebalance_tasks t
		JOIN stations s1 ON s1.id=t.from_station_id
		JOIN stations s2 ON s2.id=t.to_station_id
		WHERE (t.from_station_id=$1 OR t.to_station_id=$1 OR t.from_station_id=$2 OR t.to_station_id=$2)
		  AND t.created_at BETWEEN ($3::timestamptz - interval '6 hours') AND ($3::timestamptz + interval '6 hours')
		ORDER BY t.id`, fromSID, toSID, bt)
	if err != nil {
		log.Printf("appeal rebalance impact query error: %v", err)
	} else {
		for rbRows.Next() {
			var (
				tid, cnt          int64
				f, t, st, reason  string
				tc                time.Time
			)
			if rbRows.Scan(&tid, &f, &t, &cnt, &st, &reason, &tc) == nil {
				rebalanceImpact = append(rebalanceImpact, map[string]any{
					"task_id": tid, "from": f, "to": t, "count": cnt,
					"status": st, "reason": reason, "created_at": tc,
				})
			}
		}
		rbRows.Close()
	}

	writeJSON(w, 200, map[string]any{
		"appeal": map[string]any{
			"id": id, "ride_id": rideID, "user": userName, "reason": reason, "status": status,
			"handler": handler, "resolution": resolution, "resp_fee": respFee,
			"resp_bike": respBike, "resp_station": respSt, "refund": refund,
			"created_at": created, "resolved_at": timePtr(resolved),
		},
		"trace": map[string]any{
			"bike_code": bikeCode, "ride_status": rideStatus,
			"borrow_station": fromName, "borrow_dock_no": fromDock, "borrow_time": bt,
			"return_station": toName, "return_dock_no": toDock, "return_time": timePtr(rt),
			"duration_min": fmt.Sprintf("%.0f", durationMin), "fee": fee.Float64,
			"user_location": loc, "fault_type": ft, "fault_feedback": ff,
		},
		"cs_trace":         csTrace,
		"rebalance_impact": rebalanceImpact,
	})
}

// appealHandleHandler 责任判定：费用 / 车辆 / 站点分别定责。
func appealHandleHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Status      string  `json:"status"` // resolved / rejected
		Resolution  string  `json:"resolution"`
		RespFee     string  `json:"resp_fee"`
		RespBike    string  `json:"resp_bike"`
		RespStation string  `json:"resp_station"`
		Refund      float64 `json:"refund"`
	}
	if err := decodeBody(r, &req); err != nil || req.Resolution == "" {
		writeErr(w, 400, "请填写处理结论")
		return
	}
	if req.Status != "resolved" && req.Status != "rejected" {
		writeErr(w, 400, "状态须为 resolved 或 rejected")
		return
	}
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	var userID int64
	err := tx.QueryRow(`UPDATE appeals SET status=$1, handler_id=$2, resolution=$3,
		resp_fee=$4, resp_bike=$5, resp_station=$6, refund=$7, resolved_at=now()
		WHERE id=$8 AND status IN ('pending','processing') RETURNING user_id`,
		req.Status, u.ID, req.Resolution, req.RespFee, req.RespBike, req.RespStation,
		req.Refund, id).Scan(&userID)
	if err == sql.ErrNoRows {
		writeErr(w, 409, "申诉不存在或已处理")
		return
	} else if err != nil {
		writeErr(w, 500, "处理失败")
		return
	}
	if req.Refund > 0 {
		if _, err := tx.Exec(`UPDATE users SET balance=balance+$1 WHERE id=$2`, req.Refund, userID); err != nil {
			writeErr(w, 500, "退费失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "申诉已处理"})
}
