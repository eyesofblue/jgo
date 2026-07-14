package errors

import (
	stderrors "errors"
	"net/http"
	"testing"
)

func TestPublicValues(t *testing.T) {
	cause := stderrors.New("database password leaked in cause")
	err := Wrap(cause, 120404, "user not found", WithHTTPStatus(http.StatusNotFound))

	code, message, status := PublicValues(err)
	if code != 120404 || message != "user not found" || status != http.StatusNotFound {
		t.Fatalf("PublicValues() = %d, %q, %d", code, message, status)
	}
	if !stderrors.Is(err, cause) {
		t.Fatal("wrapped cause is not available through errors.Is")
	}
}

func TestUnknownErrorIsNotExposed(t *testing.T) {
	code, message, status := PublicValues(stderrors.New("secret internal error"))
	if code != CodeInternal || message != MessageInternal || status != http.StatusInternalServerError {
		t.Fatalf("PublicValues() = %d, %q, %d", code, message, status)
	}
}

func TestInvalidErrorFallsBackToInternal(t *testing.T) {
	err := New(0, "", WithHTTPStatus(http.StatusOK))
	if err.Code() != CodeInternal || err.Message() != MessageInternal || err.HTTPStatus() != http.StatusInternalServerError {
		t.Fatalf("invalid error was not normalized: %+v", err)
	}
}

func TestBusinessCodesAreIndependentFromHTTPStatus(t *testing.T) {
	if CodeInvalidArgument == http.StatusBadRequest || CodeInternal == http.StatusInternalServerError || CodeTimeout == http.StatusGatewayTimeout {
		t.Fatalf("business codes must not reuse HTTP status values: invalid=%d internal=%d timeout=%d", CodeInvalidArgument, CodeInternal, CodeTimeout)
	}
	if CodeOK != 0 {
		t.Fatalf("CodeOK = %d, want 0", CodeOK)
	}
}

func TestCodeOutsideInt32RangeBecomesInternal(t *testing.T) {
	err := New(CodeMax+1, "too large")
	if err.Code() != CodeInternal || err.Message() != MessageInternal {
		t.Fatalf("error = code %d message %q", err.Code(), err.Message())
	}
}
