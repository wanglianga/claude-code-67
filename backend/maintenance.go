package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var faultTypeNames = map[string]string{
	"brake": "刹车", "lock": "锁具", "chain": "链条",
	"tire": "轮胎", "seat": "坐垫", "other": "其他",
}

func faultsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	status := r.URL.Query().Get("status")
	q := `
		SELECT f.id, b.code, f.type, f.description, f.status, COALESCE(u.name,''),
		       COALESCE(s.name,''), f.created_at, f.resolved_at
		FROM faults f
		JOIN bikes b ON b.id=f.bike_id
		LEFT JOIN users u ON u.id=f.reporter_id
		LEFT JOIN stations s ON s.id=f.station_id`
	args := []any{}
	if status != "" && status != "all" {
		q += ` WHERE f.status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY f.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                     int64
			code, typ, desc, st    string
			reporter, station      string
			created                time.Time
			resolved               sql.NullTime
		)
		if err := rows.Scan(&id, &code, &typ, &desc, &st, &reporter, &station, &created, &resolved); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": code, "type": typ, "type_name": faultTypeNames[typ],
			"description": desc, "status": st, "reporter": reporter, "station": station,
			"created_at": created, "resolved_at": timePtr(resolved),
		})
	}
	writeJSON(w, 200, out)
}

func faultAssignHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	res, err := db.Exec(`UPDATE faults SET status='repairing' WHERE id=$1 AND status IN ('pending','assigned')`, id)
	if err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "故障单不存在或已在处理")
		return
	}
	db.Exec(`UPDATE bikes SET status='in_repair', station_id=NULL
		WHERE id=(SELECT bike_id FROM faults WHERE id=$1)`, id)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已接收入库维修"})
}

// createRepairHandler 维修入库登记：故障类型、用时、配件消耗、重复故障、是否报废。
func createRepairHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		FaultID    int64 `json:"fault_id"`
		DurationMin int  `json:"duration_min"`
		Result     string `json:"result"` // fixed / scrapped
		Notes      string `json:"notes"`
		Parts      []struct {
			PartID int64 `json:"part_id"`
			Qty    int   `json:"qty"`
		} `json:"parts"`
	}
	if err := decodeBody(r, &req); err != nil || req.FaultID == 0 {
		writeErr(w, 400, "参数不完整")
		return
	}
	if req.Result != "fixed" && req.Result != "scrapped" {
		writeErr(w, 400, "维修结果须为 fixed（修复）或 scrapped（报废）")
		return
	}
	var bikeID int64
	var faultType, faultStatus string
	err := db.QueryRow(`SELECT bike_id, type, status FROM faults WHERE id=$1`, req.FaultID).
		Scan(&bikeID, &faultType, &faultStatus)
	if err != nil {
		writeErr(w, 404, "故障单不存在")
		return
	}
	if faultStatus == "fixed" || faultStatus == "scrapped" {
		writeErr(w, 409, "该故障单已结案")
		return
	}
	// 重复故障检测：30 天内同车同类型已有维修记录
	var repeatCount int
	db.QueryRow(`SELECT count(*) FROM repairs r JOIN faults f ON f.id=r.fault_id
		WHERE r.bike_id=$1 AND f.type=$2 AND r.result='fixed' AND r.created_at > now()-interval '30 days'`,
		bikeID, faultType).Scan(&repeatCount)
	isRepeat := repeatCount >= 1

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	var repairID int64
	err = tx.QueryRow(`INSERT INTO repairs(fault_id,bike_id,repairer_id,finished_at,duration_min,result,notes,is_repeat)
		VALUES($1,$2,$3,now(),$4,$5,$6,$7) RETURNING id`,
		req.FaultID, bikeID, u.ID, req.DurationMin, req.Result, req.Notes, isRepeat).Scan(&repairID)
	if err != nil {
		writeErr(w, 500, "登记失败")
		return
	}
	// 配件消耗（扣减库存）
	for _, p := range req.Parts {
		if p.PartID == 0 || p.Qty <= 0 {
			continue
		}
		res, err := tx.Exec(`UPDATE parts SET stock=stock-$1 WHERE id=$2 AND stock>=$1`, p.Qty, p.PartID)
		if err != nil {
			writeErr(w, 500, "配件扣减失败")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeErr(w, 409, "配件库存不足")
			return
		}
		if _, err := tx.Exec(`INSERT INTO repair_parts(repair_id,part_id,qty) VALUES($1,$2,$3)`,
			repairID, p.PartID, p.Qty); err != nil {
			writeErr(w, 500, "配件登记失败")
			return
		}
	}
	// 故障单结案 + 车辆状态
	newFaultStatus := "fixed"
	bikeStatus := "docked"
	if req.Result == "scrapped" {
		newFaultStatus = "scrapped"
		bikeStatus = "scrapped"
	}
	if _, err := tx.Exec(`UPDATE faults SET status=$1, resolved_at=now() WHERE id=$2`, newFaultStatus, req.FaultID); err != nil {
		writeErr(w, 500, "结案失败")
		return
	}
	if req.Result == "scrapped" {
		tx.Exec(`UPDATE docks SET status='empty', bike_id=NULL WHERE bike_id=$1`, bikeID)
		tx.Exec(`UPDATE bikes SET status=$1, station_id=NULL, deploy_restricted=FALSE WHERE id=$2`, bikeStatus, bikeID)
		// 报废同步采购计划 + 更新资产台账 + 触发在站资产复核（通知维修仓）
		var bcode string
		tx.QueryRow(`SELECT code FROM bikes WHERE id=$1`, bikeID).Scan(&bcode)
		tx.Exec(`INSERT INTO procurement_plan(bike_code,reason,source_assessment_id,qty,status)
			VALUES($1,$2,NULL,1,'planned')`, bcode, fmt.Sprintf("维修判定报废（%s故障），资产退役需补充新车", faultTypeNames[faultType]))
		if assetID, err := ensureAsset(tx, bikeID); err == nil {
			agg := aggregateBike(bikeID)
			tx.Exec(`UPDATE vehicle_assets SET status='scrapped', accum_parts_cost=$1,
				accum_repair_cost=$2, mileage_km=$3, updated_at=now() WHERE id=$4`,
				agg.PartsCost, agg.Total, agg.Mileage, assetID)
		}
		reconcileOnSiteScrap(tx, 0)
	} else {
		// 修复后回到维修前所在站（若有）或保持无站点待调拨
		tx.Exec(`UPDATE bikes SET status=$1 WHERE id=$2`, bikeStatus, bikeID)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	msg := "维修完成，车辆已恢复运营"
	if req.Result == "scrapped" {
		msg = "已登记报废，车辆退出运营"
	}
	if isRepeat {
		msg += "（注意：该车 30 天内重复故障，已标记）"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "repair_id": repairID, "is_repeat": isRepeat, "message": msg})
}

func repairsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT r.id, b.code, f.type, u.name, r.started_at, r.finished_at, r.duration_min,
		       r.result, r.notes, r.is_repeat,
		       COALESCE((SELECT string_agg(p.name || '×' || rp.qty, '、')
		                 FROM repair_parts rp JOIN parts p ON p.id=rp.part_id WHERE rp.repair_id=r.id),'')
		FROM repairs r
		JOIN bikes b ON b.id=r.bike_id
		JOIN faults f ON f.id=r.fault_id
		JOIN users u ON u.id=r.repairer_id
		ORDER BY r.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, dur                 int64
			code, typ, repairer     string
			result, notes, parts    string
			started                 time.Time
			finished                sql.NullTime
			repeat                  bool
		)
		if err := rows.Scan(&id, &code, &typ, &repairer, &started, &finished, &dur, &result, &notes, &repeat, &parts); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": code, "fault_type": typ, "fault_type_name": faultTypeNames[typ],
			"repairer": repairer, "started_at": started, "finished_at": timePtr(finished),
			"duration_min": dur, "result": result, "notes": notes, "is_repeat": repeat, "parts": parts,
		})
	}
	writeJSON(w, 200, out)
}

// bikeArchiveHandler 车辆档案：全部故障与维修履历（支持按 ID 或编号查询）。
func bikeArchiveHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var (
		code, status            string
		totalRides              int
		mileage                 float64
		restricted              bool
		lastCleaned             sql.NullTime
	)
	var err error
	if _, serr := strconv.ParseInt(id, 10, 64); serr == nil {
		err = db.QueryRow(`SELECT code, status, total_rides, last_cleaned_at, COALESCE(mileage_km,0), deploy_restricted
			FROM bikes WHERE id=$1`, id).
			Scan(&code, &status, &totalRides, &lastCleaned, &mileage, &restricted)
	} else {
		err = db.QueryRow(`SELECT code, status, total_rides, last_cleaned_at, COALESCE(mileage_km,0), deploy_restricted
			FROM bikes WHERE code=$1`, id).
			Scan(&code, &status, &totalRides, &lastCleaned, &mileage, &restricted)
	}
	if err == sql.ErrNoRows {
		writeErr(w, 404, "车辆不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	history := []map[string]any{}
	rows, err := db.Query(`
		SELECT f.id, f.type, f.description, f.status, f.created_at,
		       r.id, r.duration_min, r.result, r.notes, r.is_repeat, u.name,
		       COALESCE((SELECT string_agg(p.name || '×' || rp.qty, '、')
		                 FROM repair_parts rp JOIN parts p ON p.id=rp.part_id WHERE rp.repair_id=r.id),'')
		FROM faults f
		LEFT JOIN repairs r ON r.fault_id=f.id
		LEFT JOIN users u ON u.id=r.repairer_id
		WHERE f.bike_id=(SELECT id FROM bikes WHERE code=$1)
		ORDER BY f.id DESC`, code)
	if err != nil {
		writeErr(w, 500, "查询档案失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			fid                      int64
			typ, desc, fstatus       string
			created                  time.Time
			repairID                 sql.NullInt64
			dur                      sql.NullInt64
			result, notes, repairer  sql.NullString
			repeat                   sql.NullBool
			parts                    string
		)
		if err := rows.Scan(&fid, &typ, &desc, &fstatus, &created,
			&repairID, &dur, &result, &notes, &repeat, &repairer, &parts); err != nil {
			continue
		}
		history = append(history, map[string]any{
			"fault_id": fid, "type": typ, "type_name": faultTypeNames[typ],
			"description": desc, "status": fstatus, "reported_at": created,
			"duration_min": nullInt(dur), "result": result.String, "notes": notes.String,
			"is_repeat": repeat.Bool, "repairer": repairer.String, "parts": parts,
		})
	}
	var totalPartsCost float64
	db.QueryRow(`SELECT COALESCE(sum(rp.qty*p.unit_price),0)
		FROM repair_parts rp JOIN parts p ON p.id=rp.part_id
		JOIN repairs r ON r.id=rp.repair_id WHERE r.bike_id=(SELECT id FROM bikes WHERE code=$1)`, code).Scan(&totalPartsCost)
	writeJSON(w, 200, map[string]any{
		"code": code, "status": status, "total_rides": totalRides,
		"mileage_km": mileage, "deploy_restricted": restricted,
		"total_parts_cost": totalPartsCost,
		"last_cleaned_at": timePtr(lastCleaned), "history": history,
	})
}

func partsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`SELECT id, name, stock, unit FROM parts ORDER BY id`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id    int64
			name  string
			stock int
			unit  string
		)
		if err := rows.Scan(&id, &name, &stock, &unit); err != nil {
			continue
		}
		out = append(out, map[string]any{"id": id, "name": name, "stock": stock, "unit": unit})
	}
	writeJSON(w, 200, out)
}

func maintenanceStatsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	stats := map[string]any{}
	var faultBikes, inRepair, scrapped, repeatFaults int
	db.QueryRow(`SELECT count(*) FROM bikes WHERE status='fault'`).Scan(&faultBikes)
	db.QueryRow(`SELECT count(*) FROM bikes WHERE status='in_repair'`).Scan(&inRepair)
	db.QueryRow(`SELECT count(*) FROM bikes WHERE status='scrapped'`).Scan(&scrapped)
	db.QueryRow(`SELECT count(*) FROM repairs WHERE is_repeat`).Scan(&repeatFaults)
	stats["fault_bikes"] = faultBikes
	stats["in_repair"] = inRepair
	stats["scrapped"] = scrapped
	stats["repeat_faults"] = repeatFaults
	var avg sql.NullFloat64
	db.QueryRow(`SELECT avg(duration_min) FROM repairs WHERE result='fixed'`).Scan(&avg)
	stats["avg_repair_min"] = avg.Float64
	// 按故障类型统计
	typeRows, err := db.Query(`SELECT f.type, count(*) FROM faults f GROUP BY f.type ORDER BY 2 DESC`)
	if err == nil {
		defer typeRows.Close()
		byType := []map[string]any{}
		for typeRows.Next() {
			var (
				t string
				c int
			)
			if typeRows.Scan(&t, &c) == nil {
				byType = append(byType, map[string]any{"type": t, "name": faultTypeNames[t], "count": c})
			}
		}
		stats["by_type"] = byType
	}
	writeJSON(w, 200, stats)
}
