package main

import (
	"database/sql"
	"net/http"
)

// stationJSON builds the per-station summary used by list/map/dashboard.
func stationRows() ([]map[string]any, error) {
	rows, err := db.Query(`
		SELECT s.id, s.code, s.name, s.type, s.capacity, s.location_x, s.location_y, s.address,
		       s.power_status, s.status, s.near_forbidden_zone,
		       (SELECT count(*) FROM docks d WHERE d.station_id=s.id AND d.status='occupied') AS used,
		       (SELECT count(*) FROM bikes b WHERE b.station_id=s.id AND b.status='fault') AS fault_bikes,
		       (SELECT count(*) FROM bikes b WHERE b.station_id=s.id AND b.status='docked') AS available
		FROM stations s ORDER BY s.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, cap, used, fault, avail int
			code, name, typ, addr, power, status string
			x, y float64
			nearFZ bool
		)
		if err := rows.Scan(&id, &code, &name, &typ, &cap, &x, &y, &addr, &power, &status, &nearFZ, &used, &fault, &avail); err != nil {
			return nil, err
		}
		ratio := 0.0
		if cap > 0 {
			ratio = float64(used) / float64(cap)
		}
		state := "normal"
		switch {
		case status != "normal":
			state = status
		case power != "normal":
			state = "power_outage"
		case used >= cap:
			state = "full"
		case avail <= 1:
			state = "empty"
		case ratio >= 0.85:
			state = "nearly_full"
		case ratio <= 0.15:
			state = "nearly_empty"
		}
		out = append(out, map[string]any{
			"id": id, "code": code, "name": name, "type": typ, "capacity": cap,
			"x": x, "y": y, "address": addr, "power_status": power, "status": status,
			"near_forbidden_zone": nearFZ, "used_docks": used, "free_docks": cap - used,
			"fault_bikes": fault, "available_bikes": avail, "fill_ratio": ratio, "state": state,
		})
	}
	return out, nil
}

func stationsHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	list, err := stationRows()
	if err != nil {
		writeErr(w, 500, "查询站点失败")
		return
	}
	writeJSON(w, 200, list)
}

func stationDetailHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	id := r.PathValue("id")
	var (
		name, code, typ, addr, power, status string
		cap                                  int
		nearFZ                               bool
	)
	err := db.QueryRow(`SELECT code,name,type,capacity,address,power_status,status,near_forbidden_zone
		FROM stations WHERE id=$1`, id).Scan(&code, &name, &typ, &cap, &addr, &power, &status, &nearFZ)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "站点不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	docks := []map[string]any{}
	rows, err := db.Query(`SELECT d.id, d.dock_no, d.status, COALESCE(b.code,''), COALESCE(b.status,'')
		FROM docks d LEFT JOIN bikes b ON b.id=d.bike_id
		WHERE d.station_id=$1 ORDER BY d.dock_no`, id)
	if err != nil {
		writeErr(w, 500, "查询桩位失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			did, no     int
			st, bc, bs  string
		)
		if err := rows.Scan(&did, &no, &st, &bc, &bs); err != nil {
			continue
		}
		docks = append(docks, map[string]any{
			"id": did, "dock_no": no, "status": st, "bike_code": bc, "bike_status": bs,
		})
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "code": code, "name": name, "type": typ, "capacity": cap,
		"address": addr, "power_status": power, "status": status,
		"near_forbidden_zone": nearFZ, "docks": docks,
	})
}

// mapHandler: stations + trucks for the realtime map.
func mapHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	stations, err := stationRows()
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	trucks := []map[string]any{}
	rows, err := db.Query(`SELECT t.id, t.plate, t.capacity, t.status, t.location_x, t.location_y,
		COALESCE(u.name,''), COALESCE(t.current_task_id,0)
		FROM trucks t LEFT JOIN users u ON u.id=t.driver_id ORDER BY t.id`)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, cap, taskID int
			plate, st, drv  string
			x, y            float64
		)
		if err := rows.Scan(&id, &plate, &cap, &st, &x, &y, &drv, &taskID); err != nil {
			continue
		}
		trucks = append(trucks, map[string]any{
			"id": id, "plate": plate, "capacity": cap, "status": st,
			"x": x, "y": y, "driver": drv, "task_id": taskID,
		})
	}
	writeJSON(w, 200, map[string]any{"stations": stations, "trucks": trucks})
}
