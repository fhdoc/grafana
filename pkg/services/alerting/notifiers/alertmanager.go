package notifiers

import (
	"time"
        "fmt"
	"context"
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/log"
	m "github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/alerting"
        "github.com/grafana/grafana/pkg/setting"
)

func init() {
	alerting.RegisterNotifier(&alerting.NotifierPlugin{
		Type:        "alertmanager",
		Name:        "alertmanager",
		Description: "Sends alert to Alertmanager",
		Factory:     NewAlertmanagerNotifier,
		OptionsTemplate: `
      <h3 class="page-heading">Alertmanager settings</h3>
      <div class="gf-form">
        <span class="gf-form-label width-10">Url</span>
        <input type="text" required class="gf-form-input max-width-26" ng-model="ctrl.model.settings.url" placeholder="http://localhost:9093"></input>
      </div>
    `,
	})
}

func NewAlertmanagerNotifier(model *m.AlertNotification) (alerting.Notifier, error) {
	url := model.Settings.Get("url").MustString()
	if url == "" {
		return nil, alerting.ValidationError{Reason: "Could not find url property in settings"}
	}

	return &AlertmanagerNotifier{
		NotifierBase: NewNotifierBase(model.Id, model.IsDefault, model.Name, model.Type, model.Settings),
		Url:          url,
		log:          log.New("alerting.notifier.alertmanager"),
	}, nil
}

type AlertmanagerNotifier struct {
	NotifierBase
	Url string
	log log.Logger
}

func (this *AlertmanagerNotifier) Notify(evalContext *alerting.EvalContext) error {
        sec := setting.Cfg.Section("alertmanager")
        var timeloop = sec.Key("timeloop").MustInt()

        // Loop as long as state is "alerting"
	for{
		if evalContext.Rule.State == m.AlertStateAlerting {
			ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
			this.log.Info("Sending alertmanager)
			alerts := make([]interface{}, 0)
			for _, match := range evalContext.EvalMatches {
				alertJSON := simplejson.New()
				alertJSON.Set("startsAt", evalContext.StartTime.UTC().Format(time.RFC3339))
				if evalContext.Rule.State == m.AlertStateAlerting {
					alertJSON.Set("endsAt", "0001-01-01T00:00:00Z")
				} else {
					alertJSON.Set("endsAt", evalContext.EndTime.UTC().Format(time.RFC3339))
				}
				ruleUrl, err := evalContext.GetRuleUrl()
				if err == nil {
					alertJSON.Set("generatorURL", ruleUrl)
				} else {
				 this.log.Error("Failed to get ruleUrl", "error", err, "alertmanager", this.Name)
			  }

			 if evalContext.Rule.Message != "" {
				 alertJSON.SetPath([]string{"annotations", "description"}, evalContext.Rule.Message)
			 }
			 this.log.Info("ruleUrl:", ruleUrl, " evalContext.Rule.Message: ",  evalContext.Rule.Message)

			 tags := make(map[string]string)
			 for k, v := range match.Tags {
				 tags[k] = v
			 }
			 tags["alertname"] = evalContext.Rule.Name
			 alertJSON.Set("labels", tags)
			 alerts = append(alerts, alertJSON)
		}

		bodyJSON := simplejson.NewFromAny(alerts)
		body, _ := bodyJSON.MarshalJSON()
		fmt.Println(string(body))

		cmd := &m.SendWebhookSync{
			Url:        this.Url + "/api/v1/alerts",
			HttpMethod: "POST",
			Body:       string(body),
		}

		cmdsetstate := &m.SetAlertStateCommand{
			AlertId:  evalContext.Rule.Id,
			OrgId:    evalContext.Rule.OrgId,
			State:    m.AlertStatePending,
		}

		cmdgetstate := m.GetAlertByIdQuery{}
		cmdgetstate.Id = evalContext.Rule.Id

		if err := bus.DispatchCtx(ctx, cmd); err != nil {
			this.log.Error("Failed to send alertmanager", "error", err, "alertmanager", this.Name)
			if err := bus.Dispatch(cmdsetstate); err != nil {
				this.log.Error("Failed to save state", "error", err, "RuleID= ", evalContext.Rule.Id)
			}
			return err
		}

		if err := bus.Dispatch(&cmdgetstate); err != nil {
			this.log.Error("Failed to get alert state", "error", err, "alertmanager", this.Name)
			return nil
		}

		evalContext.Rule.State  = cmdgetstate.Result.State


	} else {
		return nil
	}

        time.Sleep(time.Duration(timeloop * int(time.Second)))
 }
    return nil
}
