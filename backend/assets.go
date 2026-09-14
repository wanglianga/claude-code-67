package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// 重复故障评估关注的三大类：刹车 / 车锁 / 轮胎。
var assessFaultTypes = []string{"brake", "lock", "tire"}

func joinStr(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

const laborRatePerMin = 1.2 // 维修工时成本（元/分钟）

// bikeCostAgg 汇总单辆车的维修记录、骑行里程与配件成本。
type bikeCostAgg struct {
	Brake, Lock, Tire, RepairCnt, RepeatCnt int
	Mileage, PartsCost, LaborCost, Total    float64
}

func aggregateBike(bikeID int64) bikeCostAgg {
	var a bikeCostAgg
	// 外层为无 GROUP BY 的单行聚合，子查询只能引用参数 $1，不能引用未分组的 b.id。
	err := db.QueryRow(`
		SELECT
			count(*) FILTER (WHERE f.type='brake'),
			count(*) FILTER (WHERE f.type='lock'),
			count(*) FILTER (WHERE f.type='tire'),
			count(r.id),
			count(*) FILTER (WHERE r.is_repeat),
			(SELECT COALESCE(mileage_km,0) FROM bikes WHERE id=$1),
			COALESCE((SELECT sum(rp.qty*p.unit_price) FROM repair_parts rp
			          JOIN parts p ON p.id=rp.part_id
			          JOIN repairs r2 ON r2.id=rp.repair_id WHERE r2.bike_id=$1),0),
			COALESCE((SELECT sum(COALESCE(r3.duration_min,0))*$2::float FROM repairs r3 WHERE r3.bike_id=$1),0)
		FROM faults f
		LEFT JOIN repairs r ON r.fault_id=f.id
		WHERE f.bike_id=$1`, bikeID, laborRatePerMin).
		Scan(&a.Brake, &a.Lock, &a.Tire, &a.RepairCnt, &a.RepeatCnt, &a.Mileage, &a.PartsCost, &a.LaborCost)
	_ = err
	a.Total = a.PartsCost + a.LaborCost
	return a
}

// recommend 依据重复次数、关键故障次数、里程、累计维修成本给出系统建议。
func (a bikeCostAgg) recommend() string {
	keyFaults := a.Brake + a.Lock + a.Tire
	switch {
	case a.RepeatCnt >= 2 || keyFaults >= 3 || (a.Brake >= 2 && a.Total >= 200) ||
		(a.Total >= 300 && keyFaults >= 2) || a.Mileage >= 12000:
		return "scrap"
	case a.RepeatCnt >= 1 || keyFaults >= 2 || a.Mileage >= 8000 || a.Total >= 150:
		return "restrict"
	default:
		return "continue"
	}
}

// ---------------- 重复故障候选与评估单 ----------------

type candRow struct {
	bikeID int64
	code   string
	status string
	agg    bikeCostAgg
}

// repeatFaultCandidates 找出多次出现刹车/车锁/轮胎问题的车辆。
func repeatFaultCandidates(limit int) []candRow {
	rows, err := db.Query(`
		SELECT b.id, b.code, b.status,
		       count(*) FILTER (WHERE f.type='brake'),
		       count(*) FILTER (WHERE f.type='lock'),
		       count(*) FILTER (WHERE f.type='tire'),
		       count(r.id)
		FROM bikes b
		JOIN faults f ON f.bike_id=b.id
		LEFT JOIN repairs r ON r.fault_id=f.id
		WHERE f.type = ANY($1)
		GROUP BY b.id, b.code, b.status
		HAVING count(*) >= 2 OR count(*) FILTER (WHERE r.is_repeat) >= 1
		ORDER BY (count(*) FILTER (WHERE r.is_repeat)) DESC, count(*) DESC, b.id
		LIMIT $2`, "{"+joinStr(assessFaultTypes, ",")+"}", limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []candRow{}
	for rows.Next() {
		var c candRow
		var brake, lock, tire, repairCnt int
		if rows.Scan(&c.bikeID, &c.code, &c.status, &brake, &lock, &tire, &repairCnt) != nil {
			continue
		}
		c.agg = aggregateBike(c.bikeID)
		out = append(out, c)
	}
	return out
}

func assetCandidatesHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows := repeatFaultCandidates(50)
	out := []map[string]any{}
	for _, c := range rows {
		var openAssess int
		var lastDecision sql.NullString
		db.QueryRow(`SELECT count(*) FILTER (WHERE status='open'),
			COALESCE(MAX(decision) FILTER (WHERE status='decided'),'')
			FROM scrap_assessments WHERE bike_id=$1`, c.bikeID).Scan(&openAssess, &lastDecision)
		rec := c.agg.recommend()
		out = append(out, map[string]any{
			"bike_id": c.bikeID, "bike_code": c.code, "bike_status": c.status,
			"brake_count": c.agg.Brake, "lock_count": c.agg.Lock, "tire_count": c.agg.Tire,
			"repair_count": c.agg.RepairCnt, "repeat_count": c.agg.RepeatCnt,
			"mileage_km": c.agg.Mileage, "parts_cost": c.agg.PartsCost,
			"labor_cost": c.agg.LaborCost, "total_cost": c.agg.Total,
			"recommendation": rec, "recommendation_name": decisionName(rec),
			"open_assessment": openAssess, "last_decision": lastDecision.String,
		})
	}
	writeJSON(w, 200, out)
}

func decisionName(d string) string {
	return map[string]string{
		"pending":  "待裁决",
		"continue": "继续维修",
		"restrict": "限制投放",
		"scrap":    "报废",
	}[d]
}

// createAssessmentHandler 汇总维修记录、骑行里程、配件成本，生成报废评估单。
func createAssessmentHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		BikeID int64 `json:"bike_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.BikeID == 0 {
		writeErr(w, 400, "请选择车辆")
		return
	}
	var code, status string
	if err := db.QueryRow(`SELECT code,status FROM bikes WHERE id=$1`, req.BikeID).Scan(&code, &status); err != nil {
		writeErr(w, 404, "车辆不存在")
		return
	}
	var open int
	db.QueryRow(`SELECT count(*) FROM scrap_assessments WHERE bike_id=$1 AND status='open'`, req.BikeID).Scan(&open)
	if open > 0 {
		writeErr(w, 409, "该车已有进行中的报废评估单")
		return
	}
	agg := aggregateBike(req.BikeID)
	if agg.Brake+agg.Lock+agg.Tire < 2 && agg.RepeatCnt < 1 {
		writeErr(w, 409, "该车未达到重复故障评估条件（需刹车/车锁/轮胎问题≥2 次或存在重复维修）")
		return
	}
	var triggerFaultID sql.NullInt64
	db.QueryRow(`SELECT f.id FROM faults f WHERE f.bike_id=$1 ORDER BY f.id DESC LIMIT 1`, req.BikeID).Scan(&triggerFaultID)
	rec := agg.recommend()
	var id int64
	err := db.QueryRow(`INSERT INTO scrap_assessments
		(bike_id,trigger_fault_id,brake_count,lock_count,tire_count,repeat_count,repair_count,
		 mileage_km,parts_cost,labor_cost,total_cost,recommendation)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		req.BikeID, triggerFaultID, agg.Brake, agg.Lock, agg.Tire, agg.RepeatCnt, agg.RepairCnt,
		agg.Mileage, agg.PartsCost, agg.LaborCost, agg.Total, rec).Scan(&id)
	if err != nil {
		writeErr(w, 500, "创建评估单失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "id": id, "bike_code": code,
		"recommendation": rec, "recommendation_name": decisionName(rec),
		"message": fmt.Sprintf("已生成 %s 的报废评估单，系统建议：%s", code, decisionName(rec)),
	})
}

func assessmentsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	status := r.URL.Query().Get("status")
	q := `SELECT a.id, b.code, b.status, a.brake_count, a.lock_count, a.tire_count,
	        a.repeat_count, a.repair_count, a.mileage_km, a.parts_cost, a.labor_cost, a.total_cost,
	        a.recommendation, a.decision, COALESCE(a.decision_reason,''), COALESCE(du.name,''),
	        a.created_at, a.decided_at, COALESCE(p.id,0)
		FROM scrap_assessments a
		JOIN bikes b ON b.id=a.bike_id
		LEFT JOIN users du ON du.id=a.decided_by
		LEFT JOIN procurement_plan p ON p.id=a.procurement_id`
	args := []any{}
	if status == "open" || status == "decided" {
		q += ` WHERE a.status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY a.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询评估单失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, brake, lock, tire, repeatC, repairC int64
			procurementID                           int64
			code, bikeStatus, rec, decision, reason, decidedBy string
			mileage, parts, labor, total           float64
			created                                 time.Time
			decided                                 sql.NullTime
		)
		if rows.Scan(&id, &code, &bikeStatus, &brake, &lock, &tire, &repeatC, &repairC,
			&mileage, &parts, &labor, &total, &rec, &decision, &reason, &decidedBy,
			&created, &decided, &procurementID) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": code, "bike_status": bikeStatus,
			"brake_count": brake, "lock_count": lock, "tire_count": tire,
			"repeat_count": repeatC, "repair_count": repairC,
			"mileage_km": mileage, "parts_cost": parts, "labor_cost": labor, "total_cost": total,
			"recommendation": rec, "recommendation_name": decisionName(rec),
			"decision": decision, "decision_name": decisionName(decision),
			"decision_reason": reason, "decided_by": decidedBy,
			"created_at": created, "decided_at": timePtr(decided), "procurement_id": procurementID,
		})
	}
	writeJSON(w, 200, out)
}

// ensureAsset 取或建车辆资产台账行。
func ensureAsset(tx *sql.Tx, bikeID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM vehicle_assets WHERE bike_id=$1`, bikeID).Scan(&id)
	if err == nil {
		return id, nil
	}
	var code string
	tx.QueryRow(`SELECT code FROM bikes WHERE id=$1`, bikeID).Scan(&code)
	err = tx.QueryRow(`INSERT INTO vehicle_assets(bike_id,asset_code) VALUES($1,$2) RETURNING id`,
		bikeID, "ZC-"+code).Scan(&id)
	return id, err
}

// reconcileOnSiteScrap 扫描仍占用站点桩位但已报废/限制投放的车辆，触发资产状态复核并通知维修仓。
func reconcileOnSiteScrap(tx *sql.Tx, sourceAssessmentID int64) int {
	rows, err := tx.Query(`
		SELECT d.bike_id, b.code, d.station_id, d.id, b.status
		FROM docks d JOIN bikes b ON b.id=d.bike_id
		WHERE b.status IN ('scrapped','restricted')`)
	if err != nil {
		return 0
	}
	type sc struct {
		bikeID, stationID, dockID int64
		code, bikeStatus          string
	}
	list := []sc{}
	for rows.Next() {
		var v sc
		if rows.Scan(&v.bikeID, &v.code, &v.stationID, &v.dockID, &v.bikeStatus) == nil {
			list = append(list, v)
		}
	}
	rows.Close()
	created := 0
	for _, v := range list {
		var exists int
		tx.QueryRow(`SELECT count(*) FROM asset_reviews WHERE bike_id=$1 AND status='open'`, v.bikeID).Scan(&exists)
		if exists > 0 {
			continue
		}
		detail := fmt.Sprintf("车辆 %s 资产状态已为「%s」，但仍占用站点桩位（账面与现场不一致），请维修仓现场回收并释放桩位。",
			v.code, map[string]string{"scrapped": "报废", "restricted": "限制投放"}[v.bikeStatus])
		var sid, did any
		sid, did = v.stationID, v.dockID
		tx.Exec(`INSERT INTO asset_reviews(bike_id,bike_code,station_id,dock_id,type,detail,
			notified_warehouse,source_assessment_id)
			VALUES($1,$2,$3,$4,'scrapped_on_site',$5,TRUE,$6)`,
			v.bikeID, v.code, sid, did, detail, nullableInt64(sourceAssessmentID))
		created++
	}
	return created
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// decideAssessmentHandler 维修主管裁决：继续维修 / 限制投放 / 报废。
func decideAssessmentHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.Decision != "continue" && req.Decision != "restrict" && req.Decision != "scrap" {
		writeErr(w, 400, "裁决结果须为 continue / restrict / scrap")
		return
	}
	var bikeID int64
	var curStatus string
	if err := db.QueryRow(`SELECT bike_id,status FROM scrap_assessments WHERE id=$1`, id).
		Scan(&bikeID, &curStatus); err != nil {
		writeErr(w, 404, "评估单不存在")
		return
	}
	if curStatus != "open" {
		writeErr(w, 409, "该评估单已裁决")
		return
	}
	agg := aggregateBike(bikeID)

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()

	var procurementID sql.NullInt64
	var msg string
	switch req.Decision {
	case "continue":
		tx.Exec(`UPDATE bikes SET deploy_restricted=FALSE WHERE id=$1`, bikeID)
		msg = "裁决继续维修：车辆保留在役，资产台账状态不变"
	case "restrict":
		// 限制投放：退出站点可用车，但保留资产、等待观察
		tx.Exec(`UPDATE bikes SET status='restricted', station_id=NULL, deploy_restricted=TRUE WHERE id=$1`, bikeID)
		tx.Exec(`UPDATE docks SET status='empty', bike_id=NULL WHERE bike_id=$1`, bikeID)
		msg = "裁决限制投放：车辆已退出站点可用车预测，资产台账标记为限制投放"
	case "scrap":
		tx.Exec(`UPDATE bikes SET status='scrapped', station_id=NULL, deploy_restricted=FALSE WHERE id=$1`, bikeID)
		// 同步采购计划：报废 1 辆 → 补货 1 辆
		var code string
		tx.QueryRow(`SELECT code FROM bikes WHERE id=$1`, bikeID).Scan(&code)
		var pid int64
		tx.QueryRow(`INSERT INTO procurement_plan(bike_code,reason,source_assessment_id,qty,status)
			VALUES($1,$2,$3,1,'planned') RETURNING id`,
			code, fmt.Sprintf("车辆 %s 重复故障报废，资产退役需补充新车（累计维修成本 ¥%.0f / 里程 %.0fkm）", code, agg.Total, agg.Mileage),
			nullableInt64(mustAtoi(id))).Scan(&pid)
		procurementID = sql.NullInt64{Int64: pid, Valid: true}
		msg = fmt.Sprintf("裁决报废：资产台账已退役，已同步生成采购计划 #%d，并从站点可用车预测中剔除（防止账面有车现场无车）", pid)
	}

	assetID, _ := ensureAsset(tx, bikeID)
	assetStatus := map[string]string{"continue": "active", "restrict": "restricted", "scrap": "scrapped"}[req.Decision]
	tx.Exec(`UPDATE vehicle_assets SET status=$1, accum_parts_cost=$2, accum_repair_cost=$3,
		mileage_km=$4, last_assessment_id=$5, updated_at=now() WHERE id=$6`,
		assetStatus, agg.PartsCost, agg.Total, agg.Mileage, mustAtoi(id), assetID)

	tx.Exec(`UPDATE scrap_assessments SET status='decided',decision=$1,decision_reason=$2,
		decided_by=$3,decided_at=now(),procurement_id=$4 WHERE id=$5`,
		req.Decision, req.Reason, u.ID, procurementID, id)

	reviewsCreated := reconcileOnSiteScrap(tx, mustAtoi(id))
	if reviewsCreated > 0 {
		msg += fmt.Sprintf("；检测到 %d 辆报废/限投车辆仍在站点，已触发资产状态复核并通知维修仓", reviewsCreated)
	}

	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "裁决失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg, "asset_reviews": reviewsCreated})
}

// ---------------- 资产台账 / 采购计划 / 站点可用车预测 ----------------

func vehicleAssetsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT va.id, b.code, va.status, va.purchase_date, va.purchase_price, va.salvage_value,
		       va.accum_parts_cost, va.accum_repair_cost, va.mileage_km,
		       COALESCE(s.name,''), va.updated_at
		FROM vehicle_assets va JOIN bikes b ON b.id=va.bike_id
		LEFT JOIN stations s ON s.id=b.station_id
		ORDER BY va.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询资产台账失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                                        int64
			code, status, station                     string
			purchasePrice, salvage, partsCost, repair float64
			mileage                                   float64
			purchased                                 time.Time
			updated                                   time.Time
		)
		if rows.Scan(&id, &code, &status, &purchased, &purchasePrice, &salvage,
			&partsCost, &repair, &mileage, &station, &updated) != nil {
			continue
		}
		bookValue := purchasePrice - salvage - repair
		if bookValue < 0 {
			bookValue = 0
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": code, "status": status,
			"status_name": map[string]string{"active": "在役", "restricted": "限制投放", "scrapped": "已报废"}[status],
			"purchase_date": purchased.Format("2006-01-02"), "purchase_price": purchasePrice,
			"salvage_value": salvage, "accum_parts_cost": partsCost, "accum_repair_cost": repair,
			"mileage_km": mileage, "station": station, "book_value": bookValue, "updated_at": updated,
		})
	}
	writeJSON(w, 200, out)
}

func procurementHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`SELECT id, bike_code, reason, COALESCE(source_assessment_id,0), qty, status, created_at
		FROM procurement_plan ORDER BY id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询采购计划失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, qty, assessID int64
			code, reason, st string
			created          time.Time
		)
		if rows.Scan(&id, &code, &reason, &assessID, &qty, &st, &created) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": code, "reason": reason, "source_assessment_id": assessID,
			"qty": qty, "status": st,
			"status_name": map[string]string{"planned": "待采购", "ordered": "已下单", "received": "已到货"}[st],
			"created_at": created,
		})
	}
	writeJSON(w, 200, out)
}

// stationForecastHandler 站点可用车预测：只计在桩可投放车（剔除故障/报废/限投/在修/在途），
// 叠加分时段还/借需求，输出未来 3 小时缺口，防止“账面有车、现场无车”。
func stationForecastHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	hour := hourCN()
	rows, err := db.Query(`
		SELECT s.id, s.name, s.type, s.capacity,
		       (SELECT count(*) FROM bikes WHERE station_id=s.id AND status='docked' AND deploy_restricted=FALSE),
		       (SELECT count(*) FROM docks WHERE station_id=s.id AND status='occupied'),
		       (SELECT count(*) FROM docks d JOIN bikes b ON b.id=d.bike_id
		             WHERE d.station_id=s.id AND b.status IN ('fault','in_repair')),
		       (SELECT count(*) FROM docks d JOIN bikes b ON b.id=d.bike_id
		             WHERE d.station_id=s.id AND b.status IN ('scrapped','restricted')),
		       COALESCE((SELECT sum(borrow_need) FROM demand_profiles WHERE station_id=s.id AND hour BETWEEN $1 AND $2),0),
		       COALESCE((SELECT sum(return_need) FROM demand_profiles WHERE station_id=s.id AND hour BETWEEN $1 AND $2),0)
		FROM stations s WHERE s.status='normal' ORDER BY s.id`, hour, hour+3)
	if err != nil {
		writeErr(w, 500, "查询预测失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, cap, usable, usedDocks, faultOnDock, assetPhantom int
			name, typ                                            string
			borrowNeed, returnNeed                               int
		)
		if rows.Scan(&id, &name, &typ, &cap, &usable, &usedDocks, &faultOnDock, &assetPhantom, &borrowNeed, &returnNeed) != nil {
			continue
		}
		projected := usable + returnNeed - borrowNeed
		gap := 0
		if projected < 0 {
			gap = -projected
		}
		// 账实异常仅指报废/限投车辆仍占用桩位（应触发资产复核）；故障车在桩属正常待修，不计账实异常。
		out = append(out, map[string]any{
			"station_id": id, "station": name, "type": typ, "capacity": cap,
			"usable_bikes": usable, "occupied_docks": usedDocks,
			"fault_on_dock": faultOnDock, "asset_phantom": assetPhantom,
			"unusable_on_dock": faultOnDock + assetPhantom, "book_site_mismatch": assetPhantom > 0,
			"borrow_need_3h": borrowNeed, "return_need_3h": returnNeed,
			"projected_avail": projected, "shortage": gap,
		})
	}
	writeJSON(w, 200, out)
}

// ---------------- 资产状态复核（通知维修仓） ----------------

func assetReviewsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT ar.id, ar.bike_id, ar.bike_code, COALESCE(s.name,''), ar.type, ar.detail,
		       ar.status, ar.notified_warehouse, COALESCE(ar.warehouse_note,''),
		       COALESCE(hu.name,''), ar.created_at, ar.handled_at, COALESCE(ar.source_assessment_id,0)
		FROM asset_reviews ar
		LEFT JOIN stations s ON s.id=ar.station_id
		LEFT JOIN users hu ON hu.id=ar.handler_id
		ORDER BY ar.status, ar.id DESC`)
	if err != nil {
		writeErr(w, 500, "查询资产复核失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, bikeID, assessID        int64
			code, station, typ, detail  string
			status, note, handler       string
			notified                    bool
			created                     time.Time
			handled                     sql.NullTime
		)
		if rows.Scan(&id, &bikeID, &code, &station, &typ, &detail, &status, &notified,
			&note, &handler, &created, &handled, &assessID) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_id": bikeID, "bike_code": code, "station": station, "type": typ,
			"detail": detail, "status": status,
			"status_name": map[string]string{"open": "待维修仓处理", "acknowledged": "已受理", "resolved": "已回收销账"}[status],
			"notified_warehouse": notified, "warehouse_note": note, "handler": handler,
			"created_at": created, "handled_at": timePtr(handled), "source_assessment_id": assessID,
		})
	}
	writeJSON(w, 200, out)
}

// assetReviewHandleHandler 维修仓现场回收：释放桩位、车辆清出站点，复核销账。
func assetReviewHandleHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Action string `json:"action"` // acknowledge / resolve
		Note   string `json:"note"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	var bikeID int64
	var dockID sql.NullInt64
	var curStatus string
	if err := db.QueryRow(`SELECT bike_id,dock_id,status FROM asset_reviews WHERE id=$1`, id).
		Scan(&bikeID, &dockID, &curStatus); err != nil {
		writeErr(w, 404, "复核单不存在")
		return
	}
	if curStatus == "resolved" {
		writeErr(w, 409, "该复核单已销账")
		return
	}
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	if req.Action == "resolve" {
		if dockID.Valid {
			tx.Exec(`UPDATE docks SET status='empty', bike_id=NULL WHERE id=$1`, dockID.Int64)
		}
		tx.Exec(`UPDATE docks SET status='empty', bike_id=NULL WHERE bike_id=$1`, bikeID)
		tx.Exec(`UPDATE bikes SET station_id=NULL WHERE id=$1 AND status IN ('scrapped','restricted')`, bikeID)
		tx.Exec(`UPDATE asset_reviews SET status='resolved', handler_id=$1, warehouse_note=$2,
			handled_at=now() WHERE id=$3`, u.ID, req.Note, id)
	} else {
		tx.Exec(`UPDATE asset_reviews SET status='acknowledged', handler_id=$1, warehouse_note=$2 WHERE id=$3`,
			u.ID, req.Note, id)
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "处理失败")
		return
	}
	msg := "维修仓已受理，将安排现场回收"
	if req.Action == "resolve" {
		msg = "维修仓已现场回收车辆并释放桩位，资产复核销账，账面与现场恢复一致"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

// assessmentDetailHandler 单张评估单详情（含维修履历明细）。
func assessmentDetailHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var (
		bikeID, brake, lock, tire, repeatC, repairC         int64
		code, bikeStatus, rec, decision, reason            string
		mileage, parts, labor, total                       float64
		created                                            time.Time
	)
	err := db.QueryRow(`SELECT a.bike_id,b.code,b.status,a.brake_count,a.lock_count,a.tire_count,
		a.repeat_count,a.repair_count,a.mileage_km,a.parts_cost,a.labor_cost,a.total_cost,
		a.recommendation,a.decision,COALESCE(a.decision_reason,''),a.created_at
		FROM scrap_assessments a JOIN bikes b ON b.id=a.bike_id WHERE a.id=$1`, id).
		Scan(&bikeID, &code, &bikeStatus, &brake, &lock, &tire, &repeatC, &repairC,
			&mileage, &parts, &labor, &total, &rec, &decision, &reason, &created)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "评估单不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	// 维修履历
	records := []map[string]any{}
	rows, err := db.Query(`SELECT f.type, f.description, COALESCE(r.duration_min,0), COALESCE(r.result,''),
		COALESCE((SELECT string_agg(p.name||'×'||rp.qty,'、') FROM repair_parts rp JOIN parts p ON p.id=rp.part_id WHERE rp.repair_id=r.id),''),
		COALESCE(r.is_repeat,FALSE), f.created_at
		FROM faults f LEFT JOIN repairs r ON r.fault_id=f.id
		WHERE f.bike_id=$1 ORDER BY f.id DESC`, bikeID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				typ, desc, result, partList string
				dur                         int64
				repeat                      bool
				at                          time.Time
			)
			if rows.Scan(&typ, &desc, &dur, &result, &partList, &repeat, &at) == nil {
				records = append(records, map[string]any{
					"type": typ, "type_name": faultTypeNames[typ], "description": desc,
					"duration_min": dur, "result": result, "parts": partList,
					"is_repeat": repeat, "at": at,
				})
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"id": mustAtoi(id), "bike_id": bikeID, "bike_code": code, "bike_status": bikeStatus,
		"brake_count": brake, "lock_count": lock, "tire_count": tire,
		"repeat_count": repeatC, "repair_count": repairC,
		"mileage_km": mileage, "parts_cost": parts, "labor_cost": labor, "total_cost": total,
		"recommendation": rec, "recommendation_name": decisionName(rec),
		"decision": decision, "decision_name": decisionName(decision),
		"decision_reason": reason, "created_at": created, "records": records,
	})
}
