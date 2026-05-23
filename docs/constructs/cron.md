# Cron

Creates an EventBridge Scheduler schedule that invokes a Lambda function on a fixed schedule.

```go
import "github.com/nimbus-local/forge/constructs"

constructs.NewCron(ctx, "Cleanup", &constructs.CronArgs{
    Schedule: "rate(1 hour)",
    Job: &constructs.FunctionArgs{
        Handler: "bootstrap",
    },
})
```

---

## CronArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Schedule` | `string` | — | Required. Rate or cron expression. |
| `Job` | `*FunctionArgs` | — | Required. Lambda to invoke on the schedule. |
| `Enabled` | `*bool` | `true` | Set to `false` to create the schedule in disabled state. |

### Schedule expressions

| Format | Example |
|---|---|
| Rate | `"rate(5 minutes)"`, `"rate(1 hour)"`, `"rate(7 days)"` |
| Cron | `"cron(0 12 * * ? *)"` — every day at noon UTC |

Cron expressions use EventBridge syntax, which requires `?` for either day-of-month or day-of-week (not both).

---

## Methods

```go
func (c *Cron) FunctionARN() pulumi.StringOutput  // ARN of the invoked Lambda
func (c *Cron) ScheduleArn() pulumi.StringOutput  // ARN of the EventBridge schedule
```

---

## Example — nightly data export

```go
constructs.NewCron(ctx, "NightlyExport", &constructs.CronArgs{
    Schedule: "cron(0 2 * * ? *)",   // 02:00 UTC every day
    Job: &constructs.FunctionArgs{
        Handler:    "bootstrap",
        Timeout:    300,
        MemorySize: 512,
        Link:       []forge.Linkable{exportBucket},
    },
})
```
