package core_properties

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
)

// Trend: how the values of a property developed, from the history of the
// values – per week, how many elements had which value (choices) or the
// sum (numbers). Elements and keys follow the rights of the reader.

// TrendSeries is one value (or the sum) over the weeks.
type TrendSeries struct {
	Value  string    `json:"value"`
	Values []float64 `json:"values"`
}

// Trend is the result of POST /trend.
type Trend struct {
	Key    string        `json:"key"`
	Type   string        `json:"type"`
	Weeks  []string      `json:"weeks"` // end of each week (YYYY-MM-DD)
	Series []TrendSeries `json:"series"`
}

const maxTrendWeeks = 104

// Trend computes the development of key for the elements of q.
func (p *Plugin) Trend(ctx context.Context, q *Query, key string, weeks int) (*Trend, error) {
	if weeks <= 0 || weeks > maxTrendWeeks {
		weeks = 12
	}
	if _, builtin := builtinType[key]; builtin || key == "" {
		return nil, apperr.BadRequest("choose a property of the type selection, status, yes/no or a number")
	}
	sc, err := p.scope(ctx, q)
	if err != nil {
		return nil, err
	}
	// the reader must see every definition of the key (the history has no definition)
	vis := map[int64]bool{}
	for _, id := range sc.vis {
		vis[id] = true
	}
	for _, d := range sc.defs[key] {
		if !vis[d.ID] {
			return nil, apperr.NotFound("property")
		}
	}
	typ := p.typeOf(ctx, sc, key)
	t, _ := TypeByID(typ)
	numeric := t.Group == "number" || typ == "formula"
	if !numeric && typ != "select" && typ != "status" && typ != "boolean" {
		return nil, apperr.BadRequest("choose a property of the type selection, status, yes/no or a number")
	}
	rq := *q
	rq.Group, rq.Group2, rq.Limit, rq.Offset, rq.Columns = "", "", maxRows, 0, nil
	res, err := p.Run(ctx, &rq)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(res.Rows))
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	out := &Trend{Key: key, Type: typ, Weeks: []string{}, Series: []TrendSeries{}}
	end := time.Now().UTC().Truncate(24 * time.Hour).Add(24*time.Hour - time.Second)
	ends := make([]time.Time, weeks)
	for i := range ends {
		ends[i] = end.AddDate(0, 0, -7*(weeks-1-i))
		out.Weeks = append(out.Weeks, ends[i].Format("2006-01-02"))
	}
	if len(ids) == 0 {
		return out, nil
	}
	idx := map[string]int{}
	add := func(name string, week int, v float64) {
		i, ok := idx[name]
		if !ok {
			i = len(out.Series)
			idx[name] = i
			out.Series = append(out.Series, TrendSeries{Value: name, Values: make([]float64, weeks)})
		}
		out.Series[i].Values[week] += v
	}
	rows, err := p.host.DB.Query(ctx, `SELECT w.i, x.raw FROM unnest($3::timestamptz[]) WITH ORDINALITY AS w(d, i)
		CROSS JOIN LATERAL (SELECT DISTINCT ON (hh.element_id) hh.raw FROM plugin_prop_history hh
			WHERE hh.key = $1 AND hh.element_id = ANY($2) AND hh.changed_at <= w.d ORDER BY hh.element_id, hh.changed_at DESC) x
		WHERE x.raw IS NOT NULL`, key, ids, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var i int
		var raw []byte
		if err := rows.Scan(&i, &raw); err != nil {
			return nil, err
		}
		if numeric {
			var f float64
			if json.Unmarshal(raw, &f) == nil {
				add("sum", i-1, f)
			}
			continue
		}
		var s any
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		switch v := s.(type) {
		case string:
			add(v, i-1, 1)
		case bool:
			if v {
				add("true", i-1, 1)
			} else {
				add("false", i-1, 1)
			}
		}
	}
	return out, rows.Err()
}

func trendRoutes(p *Plugin) {
	// POST /trend {"report": {params…}, "key": "status", "weeks": 12} or {"query": {...}, …}
	p.host.Handle("POST", "/trend", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct {
			Report *reportInput `json:"report"`
			Query  *Query       `json:"query"`
			Key    string       `json:"key"`
			Weeks  int          `json:"weeks"`
		}
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		var q *Query
		switch {
		case in.Report != nil:
			var err error
			if q, err = in.Report.query(); err != nil {
				return nil, err
			}
		case in.Query != nil:
			q = in.Query
		default:
			return nil, apperr.BadRequest("report or query is required")
		}
		return p.Trend(r.Context(), q, in.Key, in.Weeks)
	})
}
