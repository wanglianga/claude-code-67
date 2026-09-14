package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// ============================ 数据结构 ============================

type routeStopInfo struct {
	ID            int64      `json:"id"`
	Seq           int        `json:"seq"`
	StationID     int64      `json:"station_id"`
	Station       string     `json:"station"`
	Kind          string     `json:"kind"`
	PlannedLoad   int        `json:"planned_load"`
	ActualLoad    int        `json:"actual_load"`
	ETA           *time.Time `json:"eta"`
	ActualArrival *time.Time `json:"actual_arrival"`
	Status        string     `json:"status"`
	Deviated      bool       `json:"deviated"`
	DeviationType string     `json:"deviation_type"`
	DeviationReason string    `json:"deviation_reason"`
	Exemption     bool       `json:"exemption"`
	Impact        string     `json:"impact"`
	// 实时站点库存快照
	AvailBikes int `json:"available_bikes"`
	UsedDocks  int `json:"used_docks"`
	Capacity   int `json:"capacity"`
	FaultBikes int `json:"fault_bikes"`
}

func stopKindName(k string) string {
	if k == "pickup" {
		return "装车（住宅区还车积压）"
	}
	return "卸车补车（地铁口取车需求）"
}

func routeStatusName(s string) string {
	return map[string]string{
		"planned": "待派车", "assigned": "已派车待出车", "executing": "执行中",
		"completed": "已完成", "cancelled": "已取消",
	}[s]
}

// ============================ 列表 / 详情 ============================

func routeListHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT r.id, r.code, r.name, r.peak_type, r.plan_date, r.status,
		       r.total_load, r.onboard, r.current_seq,
		       COALESCE(k.plate,''), COALESCE(u.name,''),
		       r.scheduled_start, r.completed_at,
		       (SELECT count(*) FROM route_stops WHERE route_id=r.id),
		       (SELECT count(*) FROM route_stops WHERE route_id=r.id AND deviated),
		       EXISTS(SELECT 1 FROM route_reviews WHERE route_id=r.id)
		FROM peak_routes r
		LEFT JOIN trucks k ON k.id=r.truck_id
		LEFT JOIN users u ON u.id=r.driver_id
		ORDER BY r.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询路线失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, load, onboard, seq, stopCnt, devCnt int64
			peakType, status, plate, driver         string
			code, name                              string
			sched                                   time.Time
			completed                               sql.NullTime
			planDate                                time.Time
			reviewed                                bool
		)
		if rows.Scan(&id, &code, &name, &peakType, &planDate, &status, &load, &onboard, &seq,
			&plate, &driver, &sched, &completed, &stopCnt, &devCnt, &reviewed) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "code": code, "name": name, "peak_type": peakType,
			"plan_date": planDate.Format("2006-01-02"), "status": status, "status_name": routeStatusName(status),
			"total_load": load, "onboard": onboard, "current_seq": seq,
			"truck": plate, "driver": driver,
			"scheduled_start": sched, "completed_at": timePtr(completed),
			"stop_count": stopCnt, "deviation_count": devCnt, "reviewed": reviewed,
		})
	}
	writeJSON(w, 200, out)
}

func loadRouteStops(routeID int64) ([]routeStopInfo, error) {
	rows, err := db.Query(`
		SELECT rs.id, rs.seq, rs.station_id, s.name, rs.kind, rs.planned_load, rs.actual_load,
		       rs.eta, rs.actual_arrival, rs.status, rs.deviated, rs.deviation_type,
		       rs.deviation_reason, rs.exemption, rs.impact,
		       (SELECT count(*) FROM bikes WHERE station_id=s.id AND status='docked'),
		       (SELECT count(*) FROM docks WHERE station_id=s.id AND status='occupied'),
		       s.capacity,
		       (SELECT count(*) FROM bikes WHERE station_id=s.id AND status='fault')
		FROM route_stops rs JOIN stations s ON s.id=rs.station_id
		WHERE rs.route_id=$1 ORDER BY rs.seq`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stops := []routeStopInfo{}
	for rows.Next() {
		var st routeStopInfo
		var eta, arr sql.NullTime
		if err := rows.Scan(&st.ID, &st.Seq, &st.StationID, &st.Station, &st.Kind, &st.PlannedLoad, &st.ActualLoad,
			&eta, &arr, &st.Status, &st.Deviated, &st.DeviationType, &st.DeviationReason,
			&st.Exemption, &st.Impact, &st.AvailBikes, &st.UsedDocks, &st.Capacity, &st.FaultBikes); err != nil {
			continue
		}
		st.ETA = timePtr(eta)
		st.ActualArrival = timePtr(arr)
		stops = append(stops, st)
	}
	return stops, nil
}

func routeDetailHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var (
		code, name, peakType, status, explanation, factors string
		planDate                                           time.Time
		capacity, totalLoad, onboard, curSeq               int
		subwayNeed, residentialBacklog                     int
		truckID                                            sql.NullInt64
		plate, driver                                      sql.NullString
		sched                                              time.Time
		started, completed                                 sql.NullTime
	)
	err := db.QueryRow(`
		SELECT code,name,peak_type,plan_date,status,capacity,total_load,onboard,current_seq,
		       subway_borrow_need,residential_backlog,explanation,factors,
		       truck_id, COALESCE((SELECT plate FROM trucks WHERE id=truck_id),''),
		       COALESCE((SELECT name FROM users WHERE id=driver_id),''),
		       scheduled_start, started_at, completed_at
		FROM peak_routes WHERE id=$1`, id).Scan(
		&code, &name, &peakType, &planDate, &status, &capacity, &totalLoad, &onboard, &curSeq,
		&subwayNeed, &residentialBacklog, &explanation, &factors,
		&truckID, &plate, &driver, &sched, &started, &completed)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "路线不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	stops, err := loadRouteStops(int64(mustAtoi(id)))
	if err != nil {
		writeErr(w, 500, "查询站点失败")
		return
	}
	var factorMap map[string]any
	json.Unmarshal([]byte(factors), &factorMap)

	// 关联高峰天气与复盘
	var weather map[string]any
	wrow := db.QueryRow(`SELECT id, peak_date, condition, temp_c, wind_level, alert_level, summary
		FROM peak_weather WHERE peak_date=$1 AND peak_type=$2`, planDate, peakType)
	var (
		wid                                                int64
		wdate                                              time.Time
		cond, wind, alert, wsummary                        string
		temp                                               float64
	)
	if wrow.Scan(&wid, &wdate, &cond, &temp, &wind, &alert, &wsummary) == nil {
		weather = map[string]any{
			"id": wid, "date": wdate.Format("2006-01-02"), "condition": cond,
			"temp_c": temp, "wind_level": wind, "alert_level": alert, "summary": wsummary,
		}
	}
	review := map[string]any{}
	var rv struct {
		id, devCnt, exemptCnt, planned, actual, shortage int64
		onTime                                           float64
		assessment, causes, improvement                  string
		created                                          time.Time
	}
	if db.QueryRow(`SELECT id,on_time_rate,deviation_count,exempt_count,driver_assessment,
		planned_total,actual_total,shortage,causes,COALESCE(improvement,''),created_at
		FROM route_reviews WHERE route_id=$1 ORDER BY id DESC LIMIT 1`, id).
		Scan(&rv.id, &rv.onTime, &rv.devCnt, &rv.exemptCnt, &rv.assessment,
			&rv.planned, &rv.actual, &rv.shortage, &rv.causes, &rv.improvement, &rv.created) == nil {
		var causes []string
		json.Unmarshal([]byte(rv.causes), &causes)
		review = map[string]any{
			"id": rv.id, "on_time_rate": rv.onTime, "deviation_count": rv.devCnt,
			"exempt_count": rv.exemptCnt, "driver_assessment": rv.assessment,
			"planned_total": rv.planned, "actual_total": rv.actual, "shortage": rv.shortage,
			"causes": causes, "improvement": rv.improvement, "created_at": rv.created,
		}
	}

	writeJSON(w, 200, map[string]any{
		"id": mustAtoi(id), "code": code, "name": name, "peak_type": peakType,
		"plan_date": planDate.Format("2006-01-02"), "status": status, "status_name": routeStatusName(status),
		"capacity": capacity, "total_load": totalLoad, "onboard": onboard, "current_seq": curSeq,
		"subway_borrow_need": subwayNeed, "residential_backlog": residentialBacklog,
		"explanation": explanation, "factors": factorMap,
		"truck_id": truckID.Int64, "truck": plate.String, "driver": driver.String,
		"scheduled_start": sched, "started_at": timePtr(started),
		"completed_at": timePtr(completed),
		"stops": stops, "weather": weather, "review": review,
	})
}

func mustAtoi(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// ============================ 早高峰前生成路线 ============================

type planStation struct {
	ID                                 int64
	Name, Typ                          string
	Cap, Avail, Used, Fault            int
	BorrowNeed, ReturnNeed             int
	Surplus, Deficit                   int
}

// reviewCongestionHits 统计历史复盘中“道路拥堵”原因出现次数，用于给下一次路线预留缓冲（复盘优化路线）。
func reviewCongestionHits() int {
	var blob string
	db.QueryRow(`SELECT COALESCE(string_agg(causes,''),'') FROM route_reviews`).Scan(&blob)
	hits := 0
	for i := 0; i+12 <= len(blob); i++ {
		if blob[i:i+12] == `"congestion"` {
			hits++
		}
	}
	return hits
}

func routePlanHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		PeakType    string `json:"peak_type"`
		Capacity    int    `json:"capacity"`
		MaxPickups  int    `json:"max_pickups"`
		MaxDropoffs int    `json:"max_dropoffs"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.PeakType != "evening" {
		req.PeakType = "morning"
	}
	if req.Capacity <= 0 {
		db.QueryRow(`SELECT COALESCE(MIN(capacity),24) FROM trucks WHERE status='idle'`).Scan(&req.Capacity)
	}
	if req.MaxPickups <= 0 {
		req.MaxPickups = 3
	}
	if req.MaxDropoffs <= 0 {
		req.MaxDropoffs = 3
	}
	// 早高峰前按 7 点时段预测取车 / 还车需求
	needHour := 7
	if req.PeakType == "evening" {
		needHour = 18
	}

	rows, err := db.Query(`
		SELECT s.id, s.name, s.type, s.capacity,
		       (SELECT count(*) FROM bikes WHERE station_id=s.id AND status='docked'),
		       (SELECT count(*) FROM docks WHERE station_id=s.id AND status='occupied'),
		       (SELECT count(*) FROM bikes WHERE station_id=s.id AND status='fault'),
		       COALESCE((SELECT borrow_need FROM demand_profiles WHERE station_id=s.id AND hour=$1),0),
		       COALESCE((SELECT return_need FROM demand_profiles WHERE station_id=s.id AND hour=$1),0)
		FROM stations s WHERE s.status='normal' ORDER BY s.id`, needHour)
	if err != nil {
		writeErr(w, 500, "读取站点需求失败")
		return
	}
	defer rows.Close()
	ps := []planStation{}
	for rows.Next() {
		var p planStation
		if rows.Scan(&p.ID, &p.Name, &p.Typ, &p.Cap, &p.Avail, &p.Used, &p.Fault,
			&p.BorrowNeed, &p.ReturnNeed) != nil {
			continue
		}
		ps = append(ps, p)
	}

	// 住宅区还车积压：预计还车后超过 70% 桩位的富余量
	for i := range ps {
		p := &ps[i]
		expected := p.Avail + p.ReturnNeed
		if p.Typ == "residential" {
			if over := expected - int(float64(p.Cap)*0.7); over > 0 {
				p.Surplus = min2(over, p.Avail)
			}
		}
		// 其他富余站也可作为补充装车点
		if p.Surplus == 0 && expected > int(float64(p.Cap)*0.85) {
			p.Surplus = min2(expected-int(float64(p.Cap)*0.7), p.Avail)
		}
		// 地铁口取车缺口：预测借车 > 当前在桩
		if p.Typ == "subway" || (req.PeakType == "evening" && p.Typ == "residential") {
			if d := p.BorrowNeed - p.Avail; d > 0 {
				p.Deficit = d
			}
		}
		// 其它明显缺车站也纳入卸车
		if p.Deficit == 0 && p.Avail <= 1 && p.BorrowNeed >= 4 {
			p.Deficit = p.BorrowNeed
		}
	}

	type loadStop struct {
		station planStation
		qty     int
	}
	pickups := []loadStop{}
	dropoffs := []loadStop{}
	capLeft := req.Capacity

	pickupCandidates := append([]planStation{}, ps...)
	// 住宅区还车积压优先作为装车点（早高峰前社区车辆堆积），其次按富余量排序
	residentialRank := func(p planStation) int {
		if p.Typ == "residential" {
			return 0
		}
		return 1
	}
	sort.SliceStable(pickupCandidates, func(i, j int) bool {
		ri, rj := residentialRank(pickupCandidates[i]), residentialRank(pickupCandidates[j])
		if ri != rj {
			return ri < rj
		}
		return pickupCandidates[i].Surplus > pickupCandidates[j].Surplus
	})
	for _, p := range pickupCandidates {
		if p.Surplus <= 0 || capLeft <= 0 || len(pickups) >= req.MaxPickups {
			continue
		}
		q := min2(p.Surplus, capLeft)
		pickups = append(pickups, loadStop{p, q})
		capLeft -= q
	}
	totalLoad := req.Capacity - capLeft
	if totalLoad == 0 {
		writeJSON(w, 200, map[string]any{"created": 0, "message": "住宅区暂无还车积压，无需生成高峰调拨路线"})
		return
	}
	remaining := totalLoad
	dropCandidates := append([]planStation{}, ps...)
	sort.SliceStable(dropCandidates, func(i, j int) bool { return dropCandidates[i].Deficit > dropCandidates[j].Deficit })
	for _, p := range dropCandidates {
		if remaining <= 0 || len(dropoffs) >= req.MaxDropoffs {
			break
		}
		if p.Deficit <= 0 {
			continue
		}
		q := min2(p.Deficit, remaining)
		dropoffs = append(dropoffs, loadStop{p, q})
		remaining -= q
	}
	if len(dropoffs) == 0 {
		writeJSON(w, 200, map[string]any{"created": 0, "message": "地铁口预测取车需求均在库存内，暂不需要补车路线"})
		return
	}
	// 若装车量超过总缺口，只按缺口装载
	if remaining > 0 {
		excess := remaining
		totalLoad -= excess
		for i := range pickups {
			take := min2(pickups[i].qty, excess)
			pickups[i].qty -= take
			excess -= take
			if excess == 0 {
				break
			}
		}
	}

	// 历史复盘学习：拥堵复盘越多，预留时间缓冲越大
	congestionHits := reviewCongestionHits()
	bufferMin := congestionHits * 3
	if bufferMin > 12 {
		bufferMin = 12
	}
	// 今日高峰天气：橙色/红色预警再追加缓冲
	var weatherAlert string
	db.QueryRow(`SELECT COALESCE(alert_level,'') FROM peak_weather
		WHERE peak_date=CURRENT_DATE AND peak_type=$1`, req.PeakType).Scan(&weatherAlert)
	if weatherAlert == "橙色" || weatherAlert == "红色" {
		bufferMin += 5
	}

	subwayNeed, backlog := 0, 0
	for _, d := range dropoffs {
		if d.station.Typ == "subway" {
			subwayNeed += d.station.Deficit
		}
	}
	for _, p := range pickups {
		if p.station.Typ == "residential" {
			backlog += p.qty
		}
	}

	tx, err := db.Begin()
	if err != nil {
		writeErr(w, 500, "开启事务失败")
		return
	}
	defer tx.Rollback()

	now := time.Now().In(locCN)
	code := fmt.Sprintf("PR-%s-%02d%02d", now.Format("0102"), now.Hour(), now.Minute())
	name := "早高峰社区→地铁口补车路线"
	if req.PeakType == "evening" {
		name = "晚高峰商圈→社区补车路线"
	}
	factors := map[string]any{
		"peak_type": req.PeakType, "truck_capacity": req.Capacity,
		"need_hour": needHour, "review_congestion_hits": congestionHits,
		"buffer_minutes": bufferMin, "weather_alert": weatherAlert,
	}
	fj, _ := json.Marshal(factors)
	explanation := fmt.Sprintf("早高峰前生成：地铁口取车需求合计约 %d 辆、当前缺口 %d 辆；住宅区还车积压约 %d 辆。",
		subwayNeed+totalLoad, subwayNeed, backlog)
	explanation += fmt.Sprintf("按调拨车容量 %d 辆，安排 %d 个装车点、%d 个卸车点，共调拨 %d 辆。",
		req.Capacity, len(pickups), len(dropoffs), totalLoad)
	if congestionHits > 0 {
		explanation += fmt.Sprintf("依据历史调拨复盘（道路拥堵 %d 次），本路线预留 %d 分钟拥堵缓冲。", congestionHits, bufferMin)
	}
	if weatherAlert == "橙色" || weatherAlert == "红色" {
		explanation += fmt.Sprintf("今日高峰天气为%s预警，额外预留 %d 分钟。", weatherAlert, 5)
	}

	var routeID int64
	err = tx.QueryRow(`INSERT INTO peak_routes(code,name,peak_type,scheduled_start,status,capacity,total_load,
		subway_borrow_need,residential_backlog,explanation,factors,created_by)
		VALUES($1,$2,$3,$4,'planned',$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		code, name, req.PeakType, now, req.Capacity, totalLoad, subwayNeed, backlog, explanation, string(fj), u.ID).
		Scan(&routeID)
	if err != nil {
		writeErr(w, 500, "创建路线失败")
		return
	}

	// 生成有序站点与计划到达时间（装车点在前、卸车点在后）
	type sstop struct {
		sid  int64
		kind string
		qty  int
	}
	ordered := []sstop{}
	for _, p := range pickups {
		if p.qty > 0 {
			ordered = append(ordered, sstop{p.station.ID, "pickup", p.qty})
		}
	}
	for _, d := range dropoffs {
		ordered = append(ordered, sstop{d.station.ID, "dropoff", d.qty})
	}
	cursor := now.Add(time.Duration(bufferMin) * time.Minute)
	prevKind := ""
	for i, ss := range ordered {
		seq := i + 1
		if i > 0 {
			cursor = cursor.Add(time.Duration(travelMinutes(prevKind, ss.kind)) * time.Minute)
		}
		eta := cursor
		tx.Exec(`INSERT INTO route_stops(route_id,seq,station_id,kind,planned_load,eta)
			VALUES($1,$2,$3,$4,$5,$6)`, routeID, seq, ss.sid, ss.kind, ss.qty, eta)
		cursor = cursor.Add(time.Duration(serviceMinutes(ss.kind)) * time.Minute)
		prevKind = ss.kind
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "保存路线失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": routeID, "code": code, "name": name, "total_load": totalLoad,
		"pickups": len(pickups), "dropoffs": len(dropoffs),
		"buffer_minutes": bufferMin,
		"message": fmt.Sprintf("已生成 %s：%d 个站点、调拨 %d 辆，等待派车", code, len(ordered), totalLoad),
	})
}

func travelMinutes(fromKind, toKind string) int {
	switch {
	case fromKind == "pickup" && toKind == "pickup":
		return 8
	case fromKind == "dropoff" && toKind == "dropoff":
		return 10
	default:
		return 12 // 住宅区 → 地铁口
	}
}
func serviceMinutes(kind string) int {
	if kind == "pickup" {
		return 6
	}
	return 7
}
func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ============================ 派车 / 出车 ============================

func routeAssignHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		TruckID int64 `json:"truck_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.TruckID == 0 {
		writeErr(w, 400, "请选择调拨车")
		return
	}
	var driverID sql.NullInt64
	var capacity int
	var truckStatus string
	if err := db.QueryRow(`SELECT driver_id, capacity, status FROM trucks WHERE id=$1`, req.TruckID).
		Scan(&driverID, &capacity, &truckStatus); err != nil {
		writeErr(w, 404, "调拨车不存在")
		return
	}
	if truckStatus != "idle" {
		writeErr(w, 409, "该调拨车正在执行其他任务")
		return
	}
	var totalLoad int
	db.QueryRow(`SELECT total_load FROM peak_routes WHERE id=$1`, id).Scan(&totalLoad)
	if capacity < totalLoad {
		writeErr(w, 409, fmt.Sprintf("调拨车容量 %d 辆小于本路线装载量 %d 辆，请更换车辆", capacity, totalLoad))
		return
	}
	res, err := db.Exec(`UPDATE peak_routes SET truck_id=$1,driver_id=$2,status='assigned'
		WHERE id=$3 AND status='planned'`, req.TruckID, driverID, id)
	if err != nil {
		writeErr(w, 500, "派车失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "路线状态不允许派车")
		return
	}
	db.Exec(`UPDATE trucks SET status='loading' WHERE id=$1`, req.TruckID)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已派车，等待司机出车执行"})
}

func routeStartHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var truckID sql.NullInt64
	if err := db.QueryRow(`SELECT truck_id FROM peak_routes WHERE id=$1`, id).Scan(&truckID); err != nil {
		writeErr(w, 404, "路线不存在")
		return
	}
	res, err := db.Exec(`UPDATE peak_routes SET status='executing',started_at=now(),current_seq=1
		WHERE id=$1 AND status='assigned'`, id)
	if err != nil {
		writeErr(w, 500, "出车失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "路线未派车或已开始")
		return
	}
	if truckID.Valid {
		db.Exec(`UPDATE trucks SET status='en_route' WHERE id=$1`, truckID.Int64)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "车辆已出车，前往第一个装车点；站点库存与预计到达将实时更新"})
}

// ============================ 到达站点（实时更新 ETA / 偏离） ============================

func routeArriveHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	seq, _ := atoiPath(r, "seq")
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()

	var status, kind, stationName string
	var stationID int64
	var eta sql.NullTime
	err := tx.QueryRow(`SELECT rs.status, rs.kind, rs.eta, s.id, s.name
		FROM route_stops rs JOIN stations s ON s.id=rs.station_id
		WHERE rs.route_id=$1 AND rs.seq=$2`, id, seq).
		Scan(&status, &kind, &eta, &stationID, &stationName)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "路线站点不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	var curSeq int
	var routeStatus string
	tx.QueryRow(`SELECT current_seq,status FROM peak_routes WHERE id=$1`, id).Scan(&curSeq, &routeStatus)
	if routeStatus != "executing" || curSeq != seq || status != "pending" {
		writeErr(w, 409, "尚未轮到该站点（请按路线顺序执行）")
		return
	}

	arrival := time.Now().In(locCN)
	// 仅晚点自动判偏离（晚点会顺延后续站点补车）；早到不损害后续补车，不计司机偏离，
	// 调度员仍可通过“补充原因”手动登记早到/其它偏离。
	deviated := false
	devType := ""
	if eta.Valid {
		if delta := arrival.Sub(eta.Time).Minutes(); delta > 2 {
			deviated, devType = true, "late"
		}
	}
	impact := ""
	if devType == "late" {
		// 偏离路线会顺延后续站点补车
		var later []string
		lr, _ := tx.Query(`SELECT s.name FROM route_stops rs JOIN stations s ON s.id=rs.station_id
			WHERE rs.route_id=$1 AND rs.seq>$2 AND rs.kind='dropoff' ORDER BY rs.seq`, id, seq)
		if lr != nil {
			for lr.Next() {
				var nm string
				if lr.Scan(&nm) == nil {
					later = append(later, nm)
				}
			}
			lr.Close()
		}
		delay := int(arrival.Sub(eta.Time).Minutes())
		if len(later) > 0 {
			impact = fmt.Sprintf("晚点 %d 分钟到达，后续卸车补车站点（%s）补车顺延，早高峰缺车窗口扩大。", delay, joinCN(later, "、"))
		} else {
			impact = fmt.Sprintf("晚点 %d 分钟到达。", delay)
		}
	}
	if _, err := tx.Exec(`UPDATE route_stops SET status='arrived',actual_arrival=$1,deviated=$2,
		deviation_type=$3,impact=$4 WHERE route_id=$5 AND seq=$6`,
		arrival, deviated, devType, impact, id, seq); err != nil {
		writeErr(w, 500, "到达登记失败")
		return
	}
	// 调拨车位置实时移动到本站
	var x, y float64
	tx.QueryRow(`SELECT location_x,location_y FROM stations WHERE id=$1`, stationID).Scan(&x, &y)
	tx.Exec(`UPDATE trucks SET location_x=$1,location_y=$2 WHERE id=(SELECT truck_id FROM peak_routes WHERE id=$3)`, x, y, id)
	recomputeETAs(tx, mustAtoi(id), seq, arrival)
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	msg := fmt.Sprintf("已到达%s（%s），站点库存已实时刷新。", stationName, stopKindName(kind))
	if deviated {
		msg += " 检测到到达偏离，请调度员补充原因；如为道路拥堵可勾选豁免，避免司机被错误考核。"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg, "deviated": deviated, "impact": impact})
}

// recomputeETAs 以 fromSeq 站实际到达时间为基准，重算后续 pending 站点预计到达。
func recomputeETAs(tx *sql.Tx, routeID int64, fromSeq int, start time.Time) {
	rows, err := tx.Query(`SELECT seq, kind FROM route_stops WHERE route_id=$1 AND seq>$2
		ORDER BY seq`, routeID, fromSeq)
	if err != nil {
		return
	}
	type sk struct {
		seq  int
		kind string
	}
	list := []sk{}
	for rows.Next() {
		var v sk
		if rows.Scan(&v.seq, &v.kind) == nil {
			list = append(list, v)
		}
	}
	rows.Close()
	var prevKind string
	tx.QueryRow(`SELECT kind FROM route_stops WHERE route_id=$1 AND seq=$2`, routeID, fromSeq).Scan(&prevKind)
	cursor := start
	for _, v := range list {
		cursor = cursor.Add(time.Duration(travelMinutes(prevKind, v.kind)) * time.Minute)
		tx.Exec(`UPDATE route_stops SET eta=$1 WHERE route_id=$2 AND seq=$3 AND status='pending'`,
			cursor, routeID, v.seq)
		cursor = cursor.Add(time.Duration(serviceMinutes(v.kind)) * time.Minute)
		prevKind = v.kind
	}
}

// ============================ 站点装卸执行（车辆状态实时更新） ============================

func routeExecuteHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	seq, _ := atoiPath(r, "seq")
	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()

	var kind, status string
	var stationID int64
	var planned int
	err := tx.QueryRow(`SELECT kind,status,station_id,planned_load FROM route_stops
		WHERE route_id=$1 AND seq=$2`, id, seq).Scan(&kind, &status, &stationID, &planned)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "路线站点不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if status != "arrived" {
		writeErr(w, 409, "请先登记到达本站，再执行装卸")
		return
	}

	var routeStatus string
	var onboard, curSeq int
	var truckID sql.NullInt64
	if err := tx.QueryRow(`SELECT status,onboard,current_seq,truck_id FROM peak_routes WHERE id=$1`, id).
		Scan(&routeStatus, &onboard, &curSeq, &truckID); err != nil || routeStatus != "executing" || curSeq != seq {
		writeErr(w, 409, "尚未轮到该站点执行")
		return
	}

	var stopID int64
	tx.QueryRow(`SELECT id FROM route_stops WHERE route_id=$1 AND seq=$2`, id, seq).Scan(&stopID)
	actual := 0
	note := ""

	if kind == "pickup" {
		// 从住宅区取在桩车装车，桩位移除、车辆转在途
		bikeRows, err := tx.Query(`SELECT id FROM bikes WHERE station_id=$1 AND status='docked'
			ORDER BY id LIMIT $2 FOR UPDATE`, stationID, planned)
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
			tx.Exec(`UPDATE docks SET status='empty',bike_id=NULL WHERE station_id=$1 AND bike_id=$2`, stationID, b)
			tx.Exec(`UPDATE bikes SET status='in_transit',station_id=NULL WHERE id=$1`, b)
			tx.Exec(`INSERT INTO route_bikes(route_id,bike_id,loaded_stop_id,status) VALUES($1,$2,$3,'onboard')`,
				mustAtoi(id), b, stopID)
			actual++
		}
		if actual < planned {
			note = fmt.Sprintf("本站可装车辆不足，实际装车 %d/%d 辆，后续地铁口补车将相应减少。", actual, planned)
			tx.Exec(`UPDATE route_stops SET deviated=TRUE,deviation_type='short',
				impact=COALESCE(NULLIF(impact,''),$1) WHERE id=$2`, note, stopID)
		}
	} else {
		// 卸车补车：车上 FIFO 车辆 → 空桩，车辆恢复在桩
		qty := min2(planned, onboard)
		rbRows, _ := tx.Query(`SELECT id,bike_id FROM route_bikes WHERE route_id=$1 AND status='onboard'
			ORDER BY id LIMIT $2 FOR UPDATE`, mustAtoi(id), qty)
		type rb struct{ recID, bikeID int64 }
		list := []rb{}
		if rbRows != nil {
			for rbRows.Next() {
				var v rb
				if rbRows.Scan(&v.recID, &v.bikeID) == nil {
					list = append(list, v)
				}
			}
			rbRows.Close()
		}
		for _, v := range list {
			var dockID int64
			if err := tx.QueryRow(`UPDATE docks SET status='occupied',bike_id=$1
				WHERE id=(SELECT id FROM docks WHERE station_id=$2 AND status='empty' ORDER BY dock_no LIMIT 1 FOR UPDATE)
				RETURNING id`, v.bikeID, stationID).Scan(&dockID); err != nil {
				continue // 站点已满
			}
			tx.Exec(`UPDATE bikes SET status='docked',station_id=$1 WHERE id=$2`, stationID, v.bikeID)
			tx.Exec(`UPDATE route_bikes SET status='unloaded',unloaded_stop_id=$1 WHERE id=$2`, stopID, v.recID)
			actual++
		}
		if actual < planned {
			short := planned - actual
			note = fmt.Sprintf("前车装车不足 / 车上余车 %d 辆，本站少补 %d 辆，早高峰地铁口仍会缺车。", onboard, short)
			tx.Exec(`UPDATE route_stops SET deviated=TRUE,deviation_type='short',
				impact=COALESCE(NULLIF(impact,''),$1) WHERE id=$2`, note, stopID)
		}
	}

	tx.Exec(`UPDATE route_stops SET status='done',actual_load=$1 WHERE id=$2`, actual, stopID)

	// 实时更新车上数量与当前站点
	newOnboard := onboard
	if kind == "pickup" {
		newOnboard += actual
	} else {
		newOnboard -= actual
	}
	nextSeq := seq + 1
	var stopTotal int
	tx.QueryRow(`SELECT count(*) FROM route_stops WHERE route_id=$1`, id).Scan(&stopTotal)
	isLast := seq >= stopTotal
	if isLast {
		tx.Exec(`UPDATE peak_routes SET onboard=$1,current_seq=$2,status='completed',completed_at=now() WHERE id=$3`,
			newOnboard, nextSeq, id)
		if truckID.Valid {
			tx.Exec(`UPDATE trucks SET status='idle' WHERE id=$1`, truckID.Int64)
		}
	} else {
		tx.Exec(`UPDATE peak_routes SET onboard=$1,current_seq=$2 WHERE id=$3`, newOnboard, nextSeq, id)
		recomputeETAs(tx, mustAtoi(id), seq, time.Now().In(locCN))
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "执行失败")
		return
	}
	verb := "装车"
	if kind == "dropoff" {
		verb = "卸车补车"
	}
	msg := fmt.Sprintf("%s完成：实际 %d 辆，站点库存与车辆状态已实时更新。", verb, actual)
	if note != "" {
		msg += " " + note
	}
	if isLast {
		msg += " 路线全部站点执行完毕，可发起调拨复盘。"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg, "actual": actual, "onboard": newOnboard, "completed": isLast})
}

// ============================ 偏离原因补充（拥堵豁免） ============================

func routeDeviationHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	seq, _ := atoiPath(r, "seq")
	var req struct {
		DeviationType string `json:"deviation_type"`
		Reason        string `json:"reason"`
		Exemption     bool   `json:"exemption"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.Reason == "" {
		writeErr(w, 400, "请补充偏离原因")
		return
	}
	if req.DeviationType == "" {
		req.DeviationType = "late"
	}
	var exists bool
	db.QueryRow(`SELECT TRUE FROM route_stops WHERE route_id=$1 AND seq=$2`, id, seq).Scan(&exists)
	if !exists {
		writeErr(w, 404, "路线站点不存在")
		return
	}
	// 道路拥堵等客观原因 → 免司机考核；非客观原因不豁免
	exemption := req.Exemption
	trafficWords := []string{"拥堵", "堵车", "管制", "事故", "封路", "积水", "暴雨", "大雪", "雾"}
	isTraffic := containsAny(req.Reason, trafficWords)
	if exemption && !isTraffic {
		writeErr(w, 409, "只有道路拥堵、交通管制、恶劣天气等客观原因才可勾选豁免司机考核")
		return
	}
	res, err := db.Exec(`UPDATE route_stops SET deviated=TRUE,deviation_type=$1,deviation_reason=$2,exemption=$3
		WHERE route_id=$4 AND seq=$5`, req.DeviationType, req.Reason, exemption, id, seq)
	if err != nil {
		writeErr(w, 500, "保存失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 404, "路线站点不存在")
		return
	}
	msg := "偏离原因已记录并进入调拨复盘"
	if isTraffic && exemption {
		msg = "已认定为道路拥堵等客观原因，本次偏离免予司机考核；原因进入调拨复盘用于优化下次路线"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if indexOf(s, w) >= 0 {
			return true
		}
	}
	return false
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
func joinCN(items []string, sep string) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += sep
		}
		out += it
	}
	return out
}
func atoiPath(r *http.Request, key string) (int, error) {
	s := r.PathValue(key)
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad int")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ============================ 调拨复盘（关联高峰天气） ============================

func routeReviewHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var status, peakType string
	var planDate time.Time
	if err := db.QueryRow(`SELECT status,peak_type,plan_date FROM peak_routes WHERE id=$1`, id).
		Scan(&status, &peakType, &planDate); err != nil {
		writeErr(w, 404, "路线不存在")
		return
	}
	if status != "completed" {
		writeErr(w, 409, "路线执行完成后才能复盘")
		return
	}
	var req struct {
		Improvement string `json:"improvement"`
	}
	decodeBody(r, &req)

	stops, err := loadRouteStops(mustAtoi(id))
	if err != nil || len(stops) == 0 {
		writeErr(w, 500, "读取站点失败")
		return
	}
	devCnt, exemptCnt, plannedTotal, actualTotal := 0, 0, 0, 0
	causes := []string{}
	addCause := func(c string) {
		for _, x := range causes {
			if x == c {
				return
			}
		}
		causes = append(causes, c)
	}
	for _, st := range stops {
		if st.Deviated {
			devCnt++
			if st.Exemption {
				exemptCnt++
				addCause("congestion") // 道路拥堵客观原因
			}
			switch st.DeviationType {
			case "late":
				addCause("late")
			case "early":
				addCause("early")
			case "short":
				addCause("short")
			}
		}
		if st.Kind == "dropoff" {
			plannedTotal += st.PlannedLoad
			actualTotal += st.ActualLoad
		}
	}
	onTime := 0
	for _, st := range stops {
		if !st.Deviated {
			onTime++
		}
	}
	onTimeRate := float64(onTime) / float64(len(stops))
	shortage := plannedTotal - actualTotal
	assessment := "normal"
	if devCnt > 0 {
		if devCnt == exemptCnt {
			assessment = "exempt" // 全部客观原因 → 不考核司机
		} else {
			assessment = "accountable"
		}
	}
	improvement := req.Improvement
	if improvement == "" {
		improvement = autoImprovement(causes, shortage)
	}
	cj, _ := json.Marshal(causes)

	// 关联当日当次高峰天气
	var weatherID sql.NullInt64
	db.QueryRow(`SELECT id FROM peak_weather WHERE peak_date=$1 AND peak_type=$2`, planDate, peakType).
		Scan(&weatherID)

	var existing int
	db.QueryRow(`SELECT count(*) FROM route_reviews WHERE route_id=$1`, id).Scan(&existing)
	if existing > 0 {
		writeErr(w, 409, "该路线已完成复盘")
		return
	}
	var reviewID int64
	err = db.QueryRow(`INSERT INTO route_reviews(route_id,weather_id,on_time_rate,deviation_count,exempt_count,
		driver_assessment,planned_total,actual_total,shortage,causes,improvement,reviewer_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		id, weatherID, onTimeRate, devCnt, exemptCnt, assessment, plannedTotal, actualTotal,
		shortage, string(cj), improvement, u.ID).Scan(&reviewID)
	if err != nil {
		writeErr(w, 500, "保存复盘失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "id": reviewID,
		"message": fmt.Sprintf("复盘已完成：准点率 %.0f%%，偏离 %d 次（拥堵豁免 %d 次），司机考核结论：%s，已关联高峰天气并用于优化下次路线",
			onTimeRate*100, devCnt, exemptCnt, assessmentName(assessment)),
		"on_time_rate": onTimeRate, "driver_assessment": assessment,
	})
}

func autoImprovement(causes []string, shortage int) string {
	out := ""
	has := func(c string) bool {
		for _, x := range causes {
			if x == c {
				return true
			}
		}
		return false
	}
	if has("congestion") || has("late") {
		out += "下一次高峰路线提前 10 分钟发车、预留拥堵缓冲，并优先选择非主干道的社区—地铁接驳通道；"
	}
	if has("short") || shortage > 0 {
		out += fmt.Sprintf("住宅区装车点实际可装车不足（少补 %d 辆），下次增加 1 个备用装车点并提前清点在桩车；", shortage)
	}
	if has("early") {
		out += "存在过早到达，下次按实际路况压缩行车时间估计；"
	}
	if out == "" {
		out = "路线准点率良好，下次高峰沿用本站序与时刻安排。"
	}
	return out
}
func assessmentName(a string) string {
	return map[string]string{"normal": "准点无偏离", "exempt": "客观原因豁免，不计考核", "accountable": "存在非客观偏离，计入考核"}[a]
}

// 偏离台账：服务于“偏离原因进入复盘、优化下一次路线”
func routeDeviationsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT r.code, r.peak_type, s.name, rs.seq, rs.kind, rs.deviation_type,
		       rs.deviation_reason, rs.exemption, rs.impact, rs.actual_arrival,
		       pw.condition, pw.alert_level
		FROM route_stops rs
		JOIN peak_routes r ON r.id=rs.route_id
		JOIN stations s ON s.id=rs.station_id
		LEFT JOIN peak_weather pw ON pw.peak_date=r.plan_date AND pw.peak_type=r.peak_type
		WHERE rs.deviated ORDER BY r.id DESC, rs.seq`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			code, peakType, sname, dtype, reason, impact, cond, alert sql.NullString
			kind                                                        string
			seq                                                         int
			exempt                                                      bool
			arr                                                         sql.NullTime
		)
		if rows.Scan(&code, &peakType, &sname, &seq, &kind, &dtype, &reason, &exempt, &impact,
			&arr, &cond, &alert) != nil {
			continue
		}
		out = append(out, map[string]any{
			"route": code.String, "peak_type": peakType.String, "station": sname.String,
			"seq": seq, "kind": kind, "deviation_type": dtype.String,
			"deviation_type_name": deviationTypeName(dtype.String),
			"reason": reason.String, "exemption": exempt, "impact": impact.String,
			"actual_arrival": timePtr(arr),
			"weather": cond.String, "weather_alert": alert.String,
		})
	}
	writeJSON(w, 200, out)
}

func deviationTypeName(t string) string {
	return map[string]string{"late": "晚点", "early": "早到", "short": "装卸不足", "skipped": "跳过站点"}[t]
}

// ============================ 高峰天气 ============================

func peakWeatherHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`SELECT id,peak_date,peak_type,condition,temp_c,wind_level,alert_level,summary
		FROM peak_weather ORDER BY peak_date DESC, peak_type DESC LIMIT 30`)
	if err != nil {
		writeErr(w, 500, "查询天气失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                          int64
			peakType, cond, wind, alert string
			date                        time.Time
			temp                        float64
			summary                     sql.NullString
		)
		if rows.Scan(&id, &date, &peakType, &cond, &temp, &wind, &alert, &summary) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "peak_date": date.Format("2006-01-02"), "peak_type": peakType,
			"condition": cond, "temp_c": temp, "wind_level": wind,
			"alert_level": alert, "summary": summary.String,
		})
	}
	writeJSON(w, 200, out)
}

func peakWeatherSetHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		PeakDate   string  `json:"peak_date"`
		PeakType   string  `json:"peak_type"`
		Condition  string  `json:"condition"`
		TempC      float64 `json:"temp_c"`
		WindLevel  string  `json:"wind_level"`
		AlertLevel string  `json:"alert_level"`
		Summary    string  `json:"summary"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.PeakDate == "" {
		req.PeakDate = time.Now().In(locCN).Format("2006-01-02")
	}
	if req.PeakType != "evening" {
		req.PeakType = "morning"
	}
	if req.Condition == "" {
		req.Condition = "晴"
	}
	if req.AlertLevel == "" {
		req.AlertLevel = "无"
	}
	_, err := db.Exec(`INSERT INTO peak_weather(peak_date,peak_type,condition,temp_c,wind_level,alert_level,summary)
		VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (peak_date,peak_type) DO UPDATE
		SET condition=EXCLUDED.condition,temp_c=EXCLUDED.temp_c,wind_level=EXCLUDED.wind_level,
		    alert_level=EXCLUDED.alert_level,summary=EXCLUDED.summary`,
		req.PeakDate, req.PeakType, req.Condition, req.TempC, req.WindLevel, req.AlertLevel, req.Summary)
	if err != nil {
		writeErr(w, 500, "保存天气失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "高峰天气已保存，复盘将自动关联"})
}

// 复盘列表
func routeReviewsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	rows, err := db.Query(`
		SELECT rv.id, r.code, r.name, r.peak_type, r.plan_date,
		       rv.on_time_rate, rv.deviation_count, rv.exempt_count, rv.driver_assessment,
		       rv.planned_total, rv.actual_total, rv.shortage, rv.causes, rv.improvement,
		       COALESCE(pw.condition,''), COALESCE(pw.alert_level,''), COALESCE(pw.wind_level,''),
		       rv.created_at
		FROM route_reviews rv
		JOIN peak_routes r ON r.id=rv.route_id
		LEFT JOIN peak_weather pw ON pw.id=rv.weather_id
		ORDER BY rv.id DESC LIMIT 100`)
	if err != nil {
		writeErr(w, 500, "查询复盘失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, devCnt, exemptCnt, planned, actual, shortage int64
			peakType, assessment, causes, improvement        string
			code, name, cond, alert, wind                    string
			onTime                                           float64
			planDate, created                                time.Time
		)
		if rows.Scan(&id, &code, &name, &peakType, &planDate, &onTime, &devCnt, &exemptCnt,
			&assessment, &planned, &actual, &shortage, &causes, &improvement,
			&cond, &alert, &wind, &created) != nil {
			continue
		}
		var causeList []string
		json.Unmarshal([]byte(causes), &causeList)
		out = append(out, map[string]any{
			"id": id, "code": code, "name": name, "peak_type": peakType,
			"plan_date": planDate.Format("2006-01-02"), "on_time_rate": onTime,
			"deviation_count": devCnt, "exempt_count": exemptCnt,
			"driver_assessment": assessment, "driver_assessment_name": assessmentName(assessment),
			"planned_total": planned, "actual_total": actual, "shortage": shortage,
			"causes": causeList, "improvement": improvement,
			"weather_condition": cond, "weather_alert": alert, "wind": wind,
			"created_at": created,
		})
	}
	writeJSON(w, 200, out)
}
