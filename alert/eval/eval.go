package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/alert/common"
	"github.com/ccfos/nightingale/v6/alert/process"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	promsdk "github.com/ccfos/nightingale/v6/pkg/prom"
	"github.com/ccfos/nightingale/v6/prom"

	"github.com/toolkits/pkg/logger"
	"github.com/toolkits/pkg/str"
)

type AlertRuleWorker struct {
	datasourceId int64
	quit         chan struct{}
	inhibit      bool
	severity     int

	rule *models.AlertRule

	processor *process.Processor

	promClients *prom.PromClientMap
	ctx         *ctx.Context
}

func NewAlertRuleWorker(rule *models.AlertRule, datasourceId int64, processor *process.Processor, promClients *prom.PromClientMap, ctx *ctx.Context) *AlertRuleWorker {
	arw := &AlertRuleWorker{
		datasourceId: datasourceId,
		quit:         make(chan struct{}),
		rule:         rule,
		processor:    processor,

		promClients: promClients,
		ctx:         ctx,
	}

	return arw
}

func (arw *AlertRuleWorker) Key() string {
	return common.RuleKey(arw.datasourceId, arw.rule.Id)
}

func (arw *AlertRuleWorker) Hash() string {
	return str.MD5(fmt.Sprintf("%d_%d_%s_%d",
		arw.rule.Id,
		arw.rule.PromEvalInterval,
		arw.rule.RuleConfig,
		arw.datasourceId,
	))
}

func (arw *AlertRuleWorker) Prepare() {
	arw.processor.RecoverAlertCurEventFromDb()
}

func (arw *AlertRuleWorker) Start() {
	logger.Infof("eval:%s started", arw.Key())
	interval := arw.rule.PromEvalInterval
	if interval <= 0 {
		interval = 10
	}
	go func() {
		for {
			select {
			case <-arw.quit:
				return
			default:
				arw.Eval()
				time.Sleep(time.Duration(interval) * time.Second)
			}
		}
	}()
}

func (arw *AlertRuleWorker) Eval() {
	cachedRule := arw.rule
	if cachedRule == nil {
		logger.Errorf("rule_eval:%s rule not found", arw.Key())
		return
	}

	typ := cachedRule.GetRuleType()
	var lst []common.AnomalyPoint
	switch typ {
	case models.PROMETHEUS:
		lst = arw.GetPromAnomalyPoint(cachedRule.RuleConfig)
	case models.HOST:
		lst = arw.GetHostAnomalyPoint(cachedRule.RuleConfig)
	default:
		return
	}

	arw.processor.Handle(lst, "inner", arw.inhibit)
}

func (arw *AlertRuleWorker) Stop() {
	logger.Infof("%s stopped", arw.Key())
	close(arw.quit)
}

func (arw *AlertRuleWorker) GetPromAnomalyPoint(ruleConfig string) []common.AnomalyPoint {
	var lst []common.AnomalyPoint
	var severity int

	var rule *models.PromRuleConfig
	if err := json.Unmarshal([]byte(ruleConfig), &rule); err != nil {
		logger.Errorf("rule_eval:%s rule_config:%s, error:%v", arw.Key(), ruleConfig, err)
		return lst
	}

	if rule == nil {
		logger.Errorf("rule_eval:%s rule_config:%s, error:rule is nil", arw.Key(), ruleConfig)
		return lst
	}

	arw.inhibit = rule.Inhibit
	for _, query := range rule.Queries {
		if query.Severity < severity {
			arw.severity = query.Severity
		}

		promql := strings.TrimSpace(query.PromQl)
		if promql == "" {
			logger.Errorf("rule_eval:%s promql is blank", arw.Key())
			continue
		}

		if arw.promClients.IsNil(arw.datasourceId) {
			logger.Errorf("rule_eval:%s error reader client is nil", arw.Key())
			continue
		}

		readerClient := arw.promClients.GetCli(arw.datasourceId)

		var warnings promsdk.Warnings
		value, warnings, err := readerClient.Query(context.Background(), promql, time.Now())
		if err != nil {
			logger.Errorf("rule_eval:%s promql:%s, error:%v", arw.Key(), promql, err)
			continue
		}

		if len(warnings) > 0 {
			logger.Errorf("rule_eval:%s promql:%s, warnings:%v", arw.Key(), promql, warnings)
			continue
		}

		logger.Debugf("rule_eval:%s query:%+v, value:%v", arw.Key(), query, value)
		points := common.ConvertAnomalyPoints(value)
		for i := 0; i < len(points); i++ {
			points[i].Severity = query.Severity
		}
		lst = append(lst, points...)
	}
	return lst
}

func (arw *AlertRuleWorker) GetHostAnomalyPoint(ruleConfig string) []common.AnomalyPoint {
	var lst []common.AnomalyPoint
	var severity int

	var rule *models.HostRuleConfig
	if err := json.Unmarshal([]byte(ruleConfig), &rule); err != nil {
		logger.Errorf("rule_eval:%s rule_config:%s, error:%v", arw.Key(), ruleConfig, err)
		return lst
	}

	if rule == nil {
		logger.Errorf("rule_eval:%s rule_config:%s, error:rule is nil", arw.Key(), ruleConfig)
		return lst
	}

	arw.inhibit = rule.Inhibit
	now := time.Now().Unix()
	for _, trigger := range rule.Triggers {
		if trigger.Severity < severity {
			arw.severity = trigger.Severity
		}

		query := models.GetHostsQuery(rule.Queries)
		switch trigger.Type {
		case "target_miss", "offset":
			t := now - int64(trigger.Duration)
			if trigger.Type == "offset" {
				t = int64(trigger.Duration)
			}

			hosts, err := models.TargetGetsByFilter(arw.ctx, query, trigger.Type, t, 0, 0)
			if err != nil {
				logger.Errorf("rule_eval:%s query:%v, error:%v", arw.Key(), query, err)
				continue
			}

			for _, host := range hosts {
				m := make(map[string]string)
				m["ident"] = host.Ident
				lst = append(lst, common.NewAnomalyPoint(trigger.Type, m, now, float64(t), trigger.Severity))
			}
		case "pct_target_miss":
			AllCount, err := models.TargetCountByFilter(arw.ctx, query, "", 0)
			if err != nil {
				logger.Errorf("rule_eval:%s query:%v, error:%v", arw.Key(), query, err)
				continue
			}
			missCount, err := models.TargetCountByFilter(arw.ctx, query, trigger.Type, now-int64(trigger.Duration))
			if err != nil {
				logger.Errorf("rule_eval:%s query:%v, error:%v", arw.Key(), query, err)
				continue
			}

			pct := float64(missCount) / float64(AllCount) * 100
			if pct >= float64(trigger.Percent) {
				lst = append(lst, common.NewAnomalyPoint(trigger.Type, nil, now, pct, trigger.Severity))
			}
		}
	}
	return lst
}
