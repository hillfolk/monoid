package requestworkflow

import (
	"time"

	"github.com/monoid-privacy/monoid/model"
	"github.com/monoid-privacy/monoid/workflow/activity"
	"github.com/monoid-privacy/monoid/workflow/activity/requestactivity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type ExecuteRequestArgs struct {
	RequestID   string
	JobID       string
	WorkspaceID string
}

func (w *RequestWorkflow) ExecuteRequestWorkflow(
	ctx workflow.Context,
	args ExecuteRequestArgs,
) (err error) {
	logger := workflow.GetLogger(ctx)

	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 2,
		},
	}

	ctx = workflow.WithActivityOptions(ctx, options)

	reqAc := requestactivity.RequestActivity{}
	status := model.JobStatusCompleted
	hasSuccess := false

	cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
	defer func() {
		ac := activity.Activity{}

		if err != nil {
			status = model.JobStatusFailed
		}

		if !hasSuccess && status == model.JobStatusPartialFailed {
			status = model.JobStatusFailed
		}

		terr := workflow.ExecuteActivity(cleanupCtx, ac.UpdateJobStatus, activity.JobStatusInput{
			ID:     args.JobID,
			Status: status,
		}).Get(ctx, nil)

		if terr != nil && err == nil {
			err = terr
		}
	}()

	silos := []model.SiloDefinition{}
	if err := workflow.ExecuteActivity(ctx, reqAc.FindDBSilos, requestactivity.FindRequestArgs{
		WorkspaceID: args.WorkspaceID,
	}).Get(ctx, &silos); err != nil {
		return err
	}

	ctx = workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{})
	sel := workflow.NewSelector(ctx)

	for i, silo := range silos {
		i := i

		sel.AddFuture(workflow.ExecuteChildWorkflow(ctx, w.ExecuteSiloRequestWorkflow, SiloRequestArgs{
			RequestID:        args.RequestID,
			SiloDefinitionID: silo.ID,
		}), func(f workflow.Future) {
			res := ExecuteSiloRequestResult{}

			if err := f.Get(ctx, &res); err != nil {
				logger.Error("error scheduling execute request", map[string]interface{}{
					"silo": silos[i],
				}, err)

				status = model.JobStatusPartialFailed
			}

			switch res.Status {
			case model.FullRequestStatusFailed:
				status = model.JobStatusPartialFailed
			case model.FullRequestStatusPartialFailed:
				status = model.JobStatusPartialFailed
				hasSuccess = true
			case model.FullRequestStatusExecuted:
				hasSuccess = true
			}
		})
	}

	for range silos {
		sel.Select(ctx)
	}

	return nil
}
