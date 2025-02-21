package requestworkflow

import (
	"time"

	"github.com/monoid-privacy/monoid/model"
	"github.com/monoid-privacy/monoid/monoidprotocol"
	"github.com/monoid-privacy/monoid/workflow/activity/requestactivity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type SiloRequestArgs struct {
	SiloDefinitionID string `json:"siloDefinitionId"`
	RequestID        string `json:"requestId"`
}

const pollTime = 1 * time.Hour

func updateRequest(ctx workflow.Context, requestStatusID string, status model.RequestStatusType) error {
	ac := requestactivity.RequestActivity{}

	err := workflow.ExecuteActivity(
		ctx,
		ac.UpdateRequestStatusActivity,
		requestactivity.UpdateRequestStatusArgs{
			RequestStatusID: requestStatusID,
			Status:          status,
		}).Get(ctx, nil)

	return err
}

type ExecuteSiloRequestResult struct {
	Status model.FullRequestStatus
}

func (w *RequestWorkflow) ExecuteSiloRequestWorkflow(
	ctx workflow.Context,
	args SiloRequestArgs,
) (requestRes ExecuteSiloRequestResult, err error) {
	logger := workflow.GetLogger(ctx)
	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
		},
	}

	ctx = workflow.WithActivityOptions(ctx, options)

	ac := requestactivity.RequestActivity{}

	reqStatus := requestactivity.RequestStatusResult{}
	requestRes = ExecuteSiloRequestResult{Status: model.FullRequestStatusFailed}

	if err := workflow.ExecuteActivity(ctx, ac.StartSiloRequestActivity, requestactivity.StartRequestArgs{
		SiloDefinitionID: args.SiloDefinitionID,
		RequestID:        args.RequestID,
	}).Get(ctx, &reqStatus); err != nil {
		if err := workflow.ExecuteActivity(
			ctx,
			ac.BatchUpdateRequestStatusActivity,
			requestactivity.BatchUpdateRequestStatusArgs{
				RequestID:        args.RequestID,
				SiloDefinitionID: args.SiloDefinitionID,
				Status:           model.RequestStatusTypeFailed,
			},
		).Get(ctx, nil); err != nil {
			return requestRes, err
		}

		return requestRes, nil
	}

	processing := reqStatus.ResultItems
	hasFailures := false

	for len(processing) > 0 {
		newProcessing := make([]requestactivity.RequestStatusItem, 0, len(processing))
		type resultExtractTuple struct {
			status          monoidprotocol.MonoidRequestStatus
			requestStatusID string
		}
		resultExtractData := []resultExtractTuple{}

		for _, res := range processing {
			// If running the start actions for the request resulted in an error, fail the request
			if res.Error != nil {
				hasFailures = true

				if terr := updateRequest(ctx, res.RequestStatusID, model.RequestStatusTypeFailed); terr != nil {
					logger.Error("Error updating request", terr)
				}

				continue
			}

			// If the request was already completed, without needing any processing, then mark it as
			// executed.
			if res.FullyComplete {
				requestRes.Status = model.FullRequestStatusPartialFailed

				if terr := updateRequest(ctx, res.RequestStatusID, model.RequestStatusTypeExecuted); terr != nil {
					logger.Error("Error updating request", terr)
				}

				continue
			}

			switch res.RequestStatus.RequestStatus {
			case monoidprotocol.MonoidRequestStatusRequestStatusCOMPLETE:
				// If the request is complete, and it's not already marked as fully complete, then
				// add it to the list of data that needs further processing
				resultExtractData = append(resultExtractData, resultExtractTuple{
					status:          *res.RequestStatus,
					requestStatusID: res.RequestStatusID,
				})
			case monoidprotocol.MonoidRequestStatusRequestStatusFAILED:
				hasFailures = true
				if terr := updateRequest(ctx, res.RequestStatusID, model.RequestStatusTypeFailed); terr != nil {
					logger.Error("Error updating request", terr)
					continue
				}
			case monoidprotocol.MonoidRequestStatusRequestStatusPROGRESS:
				// These will be re-visited on the next iteration
				newProcessing = append(newProcessing, res)
			}
		}

		// Extract the data that is complete and needed further processing
		if len(resultExtractData) > 0 {
			requestArgs := requestactivity.ProcessRequestArgs{
				ProtocolRequestStatus: make([]monoidprotocol.MonoidRequestStatus, len(resultExtractData)),
				RequestStatusIDs:      make([]string, len(resultExtractData)),
			}

			for i, datum := range resultExtractData {
				requestArgs.ProtocolRequestStatus[i] = datum.status
				requestArgs.RequestStatusIDs[i] = datum.requestStatusID
			}

			res := requestactivity.ProcessRequestResult{}

			logger.Info("Calling process")
			// Call the process activity to get data.
			if err := workflow.ExecuteActivity(ctx, ac.ProcessRequestResults, requestArgs).Get(ctx, &res); err != nil {
				// If the activity itself fails, then all the input needs to be marked as failed.
				logger.Error("Error processing results", err)

				hasFailures = true

				for _, datum := range resultExtractData {
					if terr := updateRequest(ctx, datum.requestStatusID, model.RequestStatusTypeFailed); terr != nil {
						logger.Error("Error updating request", terr)
						continue
					}
				}
			} else {
				// Update the request statuses in the DB
				for _, r := range res.ResultItems {
					status := model.RequestStatusTypeExecuted

					if r.Error != nil {
						hasFailures = true
						status = model.RequestStatusTypeFailed
					} else {
						requestRes.Status = model.FullRequestStatusPartialFailed
					}

					if terr := updateRequest(ctx, r.RequestStatusID, status); terr != nil {
						logger.Error("Error updating request", terr)
						continue
					}
				}
			}
		}

		if len(newProcessing) == 0 {
			break
		}

		// Sleep before getting the new statuses, so we aren't hitting apis too frequently.
		if err := workflow.Sleep(ctx, pollTime); err != nil {
			return ExecuteSiloRequestResult{}, err
		}

		// Run another request to get the status, and process again.
		statusIDs := make([]string, 0, len(newProcessing))
		for _, r := range newProcessing {
			statusIDs = append(statusIDs, r.RequestStatusID)
		}

		res := requestactivity.RequestStatusResult{}

		if err := workflow.ExecuteActivity(ctx, ac.RequestStatusActivity, requestactivity.RequestStatusArgs{
			RequestStatusIDs: statusIDs,
		}).Get(ctx, &res); err != nil {
			return requestRes, err
		}

		processing = res.ResultItems
	}

	if !hasFailures {
		requestRes.Status = model.FullRequestStatusExecuted
	}

	return requestRes, nil
}
