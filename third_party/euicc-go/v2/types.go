package sgp22

import (
	"net/url"
	"strings"

	"github.com/damonto/euicc-go/bertlv"
)

type Transmitter interface {
	Transmit(bertlv.Marshaler, bertlv.Unmarshaler) error
	TransmitRaw([]byte) ([]byte, error)
}

type CardRequest[R CardResponse] interface {
	bertlv.Marshaler
	CardResponse() R
}

type CardResponse interface {
	bertlv.Unmarshaler
	Valid() error
}

func InvokeAPDU[I CardRequest[O], O CardResponse](transmitter Transmitter, request I) (O, error) {
	response := request.CardResponse()
	err := transmitter.Transmit(request, response)
	if err == nil {
		err = response.Valid()
	}
	return response, err
}

func InvokeRawAPDU(transmitter Transmitter, command []byte) ([]byte, error) {
	return transmitter.TransmitRaw(command)
}

type HTTPClient interface {
	SendRequest(url *url.URL, request, response any) error
}

type HTTPRequest[R HTTPResponse] interface {
	URL(*url.URL) *url.URL
	RemoteResponse() R
}

type HTTPResponse interface {
	FunctionExecutionStatus() *ExecutionStatus
}

func InvokeHTTP[I HTTPRequest[O], O HTTPResponse](client HTTPClient, address *url.URL, request I) (O, error) {
	response := request.RemoteResponse()
	endpoint := request.URL(address)
	if err := client.SendRequest(endpoint, request, response); err != nil {
		return response, err
	}
	status := response.FunctionExecutionStatus()
	if !status.ExecutedSuccess() {
		endpointPath := endpoint.EscapedPath()
		if endpointPath != "" && !strings.HasPrefix(endpointPath, "/") {
			endpointPath = "/" + endpointPath
		}
		remoteError := &RemoteExecutionError{
			Endpoint: endpointPath,
		}
		if status != nil {
			remoteError.Status = status.Status
			if status.StatusCodeData != nil {
				remoteError.SubjectCode = status.StatusCodeData.SubjectCode
				remoteError.ReasonCode = status.StatusCodeData.ReasonCode
				remoteError.Message = status.StatusCodeData.Message
			}
		}
		return response, remoteError
	}
	return response, nil
}
