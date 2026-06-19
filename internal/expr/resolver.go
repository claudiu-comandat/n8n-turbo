package expr

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/n8n-io/n8n-turbo/internal/dataplane"
)

type Resolver struct {
	pool    sync.Pool
	timeout time.Duration
}

var templateExpression = regexp.MustCompile(`=\{\{\s*(.*?)\s*\}\}`)

func NewResolver(timeout time.Duration) *Resolver {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	resolver := &Resolver{timeout: timeout}
	resolver.pool.New = func() any {
		return goja.New()
	}
	return resolver
}

func (r *Resolver) Resolve(raw any, exprCtx Context) (any, error) {
	switch value := raw.(type) {
	case string:
		return r.resolveString(value, exprCtx)
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			resolved, err := r.Resolve(item, exprCtx)
			if err != nil {
				return nil, err
			}
			result[key] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			resolved, err := r.Resolve(item, exprCtx)
			if err != nil {
				return nil, err
			}
			result[index] = resolved
		}
		return result, nil
	default:
		return raw, nil
	}
}

func (r *Resolver) MustResolve(raw any, exprCtx Context) any {
	resolved, err := r.Resolve(raw, exprCtx)
	if err != nil {
		return raw
	}
	return resolved
}

func (r *Resolver) resolveString(value string, exprCtx Context) (any, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "={{") && strings.HasSuffix(trimmed, "}}") && templateExpression.MatchString(trimmed) {
		code := strings.TrimSuffix(strings.TrimPrefix(trimmed, "={{"), "}}")
		return r.evaluate(strings.TrimSpace(code), exprCtx)
	}
	if !templateExpression.MatchString(value) {
		return value, nil
	}
	result := templateExpression.ReplaceAllStringFunc(value, func(match string) string {
		groups := templateExpression.FindStringSubmatch(match)
		if len(groups) != 2 {
			return match
		}
		evaluated, err := r.evaluate(groups[1], exprCtx)
		if err != nil {
			return match
		}
		return fmt.Sprint(evaluated)
	})
	return result, nil
}

func (r *Resolver) evaluate(code string, exprCtx Context) (any, error) {
	vm := r.pool.Get().(*goja.Runtime)
	timer := time.AfterFunc(r.timeout, func() {
		vm.Interrupt("expression timeout")
	})
	defer func() {
		timer.Stop()
		r.release(vm)
	}()
	if err := r.inject(vm, exprCtx); err != nil {
		return nil, err
	}
	value, err := vm.RunString("(" + code + ")")
	vm.ClearInterrupt()
	if err != nil {
		return nil, err
	}
	return value.Export(), nil
}

func (r *Resolver) release(vm *goja.Runtime) {
	for _, name := range []string{"$json", "$binary", "$vars", "$secrets", "$now", "$today", "$itemIndex", "$runIndex", "$workflow", "$execution", "$input", "$node", "$json_stringify", "console", "DateTime", "N8nDateTime", "__n8nNow"} {
		_ = vm.Set(name, goja.Undefined())
	}
	r.pool.Put(vm)
}

func (r *Resolver) inject(vm *goja.Runtime, exprCtx Context) error {
	now := exprCtx.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	item := exprCtx.CurrentItem()
	_ = vm.Set("$json", item.JSON)
	_ = vm.Set("$binary", item.Binary)
	_ = vm.Set("$vars", exprCtx.Variables)
	_ = vm.Set("$secrets", exprCtx.Secrets)
	_ = vm.Set("__n8nNow", now.UnixMilli())
	_ = vm.Set("$itemIndex", exprCtx.CurrentIndex)
	_ = vm.Set("$runIndex", exprCtx.RunIndex)
	_ = vm.Set("$workflow", map[string]any{"id": exprCtx.WorkflowID, "name": exprCtx.WorkflowName})
	execution := map[string]any{"id": exprCtx.ExecutionID, "mode": exprCtx.ExecutionMode, "resumeUrl": exprCtx.ResumeURL, "resumeFormUrl": exprCtx.ResumeFormURL}
	if !exprCtx.ScheduledTime.IsZero() {
		execution["scheduledTime"] = exprCtx.ScheduledTime.UTC().Format(time.RFC3339Nano)
	}
	_ = vm.Set("$execution", execution)
	_ = vm.Set("$input", map[string]any{
		"item": map[string]any{"json": item.JSON, "binary": item.Binary, "pairedItem": item.PairedItem},
		"first": func() any {
			if len(exprCtx.Items) == 0 {
				return nil
			}
			return map[string]any{"json": exprCtx.Items[0].JSON, "binary": exprCtx.Items[0].Binary}
		},
		"all": func() []map[string]any {
			result := make([]map[string]any, 0, len(exprCtx.Items))
			for _, current := range exprCtx.Items {
				result = append(result, map[string]any{"json": current.JSON, "binary": current.Binary})
			}
			return result
		},
	})
	_ = vm.Set("$node", nodeData(exprCtx.RunData))
	_ = vm.Set("$json_stringify", func(value any) string {
		bytes, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(bytes)
	})
	_ = vm.Set("console", map[string]any{"log": func(args ...any) {}})
	_, err := vm.RunString(luxonCompatScript)
	return err
}

const luxonCompatScript = `
globalThis.N8nDateTime = class {
	constructor(value) {
		this._date = value instanceof Date ? new Date(value.getTime()) : new Date(value);
	}
	toISO() {
		return this._date.toISOString();
	}
	toFormat(format) {
		const pad = (value, length = 2) => String(value).padStart(length, '0');
		return String(format)
			.replace(/yyyy/g, String(this._date.getUTCFullYear()))
			.replace(/MM/g, pad(this._date.getUTCMonth() + 1))
			.replace(/dd/g, pad(this._date.getUTCDate()))
			.replace(/HH/g, pad(this._date.getUTCHours()))
			.replace(/mm/g, pad(this._date.getUTCMinutes()))
			.replace(/ss/g, pad(this._date.getUTCSeconds()))
			.replace(/SSS/g, pad(this._date.getUTCMilliseconds(), 3));
	}
	plus(duration) {
		const d = new Date(this._date.getTime());
		duration = duration || {};
		if (duration.years) d.setUTCFullYear(d.getUTCFullYear() + duration.years);
		if (duration.months) d.setUTCMonth(d.getUTCMonth() + duration.months);
		if (duration.weeks) d.setUTCDate(d.getUTCDate() + duration.weeks * 7);
		if (duration.days) d.setUTCDate(d.getUTCDate() + duration.days);
		if (duration.hours) d.setUTCHours(d.getUTCHours() + duration.hours);
		if (duration.minutes) d.setUTCMinutes(d.getUTCMinutes() + duration.minutes);
		if (duration.seconds) d.setUTCSeconds(d.getUTCSeconds() + duration.seconds);
		if (duration.milliseconds) d.setUTCMilliseconds(d.getUTCMilliseconds() + duration.milliseconds);
		return new globalThis.N8nDateTime(d);
	}
	minus(duration) {
		const negated = {};
		for (const key of Object.keys(duration || {})) negated[key] = -duration[key];
		return this.plus(negated);
	}
	startOf(unit) {
		const d = new Date(this._date.getTime());
		if (unit === 'year') {
			d.setUTCMonth(0, 1);
			d.setUTCHours(0, 0, 0, 0);
		}
		if (unit === 'month') {
			d.setUTCDate(1);
			d.setUTCHours(0, 0, 0, 0);
		}
		if (unit === 'week') {
			const day = d.getUTCDay() || 7;
			d.setUTCDate(d.getUTCDate() - day + 1);
			d.setUTCHours(0, 0, 0, 0);
		}
		if (unit === 'day') d.setUTCHours(0, 0, 0, 0);
		if (unit === 'hour') d.setUTCMinutes(0, 0, 0);
		if (unit === 'minute') d.setUTCSeconds(0, 0);
		if (unit === 'second') d.setUTCMilliseconds(0);
		return new globalThis.N8nDateTime(d);
	}
	endOf(unit) {
		if (unit === 'year') return this.startOf('year').plus({ years: 1 }).minus({ milliseconds: 1 });
		if (unit === 'month') return this.startOf('month').plus({ months: 1 }).minus({ milliseconds: 1 });
		if (unit === 'week') return this.startOf('week').plus({ weeks: 1 }).minus({ milliseconds: 1 });
		if (unit === 'day') return this.startOf('day').plus({ days: 1 }).minus({ milliseconds: 1 });
		if (unit === 'hour') return this.startOf('hour').plus({ hours: 1 }).minus({ milliseconds: 1 });
		if (unit === 'minute') return this.startOf('minute').plus({ minutes: 1 }).minus({ milliseconds: 1 });
		if (unit === 'second') return this.startOf('second').plus({ seconds: 1 }).minus({ milliseconds: 1 });
		return new globalThis.N8nDateTime(this._date);
	}
	get ts() {
		return this._date.getTime();
	}
	get year() {
		return this._date.getUTCFullYear();
	}
	get month() {
		return this._date.getUTCMonth() + 1;
	}
	get day() {
		return this._date.getUTCDate();
	}
	get hour() {
		return this._date.getUTCHours();
	}
	get minute() {
		return this._date.getUTCMinutes();
	}
	get second() {
		return this._date.getUTCSeconds();
	}
	get weekday() {
		return this._date.getUTCDay() || 7;
	}
	valueOf() {
		return this._date.getTime();
	}
	toString() {
		return this.toISO();
	}
	toJSON() {
		return this.toISO();
	}
}
globalThis.DateTime = {
	now: () => new globalThis.N8nDateTime(new Date(globalThis.__n8nNow)),
	fromISO: (value) => new globalThis.N8nDateTime(new Date(value)),
	fromMillis: (value) => new globalThis.N8nDateTime(new Date(value)),
	fromJSDate: (value) => new globalThis.N8nDateTime(value),
	local: (year, month = 1, day = 1, hour = 0, minute = 0, second = 0, millisecond = 0) => new globalThis.N8nDateTime(new Date(Date.UTC(year, month - 1, day, hour, minute, second, millisecond))),
}
globalThis.$now = globalThis.DateTime.now();
globalThis.$today = globalThis.DateTime.now().startOf('day');
`

func nodeData(runData map[string][]dataplane.TaskData) map[string]any {
	result := make(map[string]any, len(runData))
	for nodeName, tasks := range runData {
		if len(tasks) == 0 {
			continue
		}
		outputs := tasks[len(tasks)-1].Data["main"]
		if len(outputs) == 0 || len(outputs[0]) == 0 {
			result[nodeName] = map[string]any{"json": map[string]any{}}
			continue
		}
		result[nodeName] = map[string]any{"json": outputs[0][0].JSON}
	}
	return result
}
