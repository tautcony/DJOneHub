package sgp22

import (
	"errors"
	"net/url"
	"testing"
)

type remoteExecutionTestResponse struct {
	status *ExecutionStatus
}

func (r *remoteExecutionTestResponse) FunctionExecutionStatus() *ExecutionStatus {
	return r.status
}

type remoteExecutionTestRequest struct {
	response *remoteExecutionTestResponse
}

func (r *remoteExecutionTestRequest) URL(address *url.URL) *url.URL {
	return address.JoinPath("/gsma/rsp2/es9plus/authenticateClient")
}

func (r *remoteExecutionTestRequest) RemoteResponse() *remoteExecutionTestResponse {
	return r.response
}

type remoteExecutionTestHTTPClient struct{}

func (remoteExecutionTestHTTPClient) SendRequest(_ *url.URL, _, _ any) error { return nil }

func TestInvokeHTTPPreservesRemoteExecutionDiagnostics(t *testing.T) {
	address, err := url.Parse("https://smdp.example.com")
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	request := &remoteExecutionTestRequest{response: &remoteExecutionTestResponse{status: &ExecutionStatus{
		Status: "Failed",
		StatusCodeData: &StatusCodeData{
			SubjectCode: "8.2.6",
			ReasonCode:  "3.8",
			Message:     "Execution Failed.",
		},
	}}}

	_, err = InvokeHTTP(remoteExecutionTestHTTPClient{}, address, request)
	var remoteError *RemoteExecutionError
	if !errors.As(err, &remoteError) {
		t.Fatalf("InvokeHTTP() error = %T %v, want *RemoteExecutionError", err, err)
	}
	if remoteError.Endpoint != "/gsma/rsp2/es9plus/authenticateClient" ||
		remoteError.Status != "Failed" ||
		remoteError.SubjectCode != "8.2.6" ||
		remoteError.ReasonCode != "3.8" ||
		remoteError.Message != "Execution Failed." {
		t.Fatalf("remote error fields: endpoint=%q status=%q subject=%q reason=%q message=%q",
			remoteError.Endpoint, remoteError.Status, remoteError.SubjectCode, remoteError.ReasonCode, remoteError.Message)
	}
}
