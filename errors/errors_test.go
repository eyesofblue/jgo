package errors

import (
	stderrors "errors"
	"net/http"
	"strings"
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

func TestCatalogRejectsDuplicatesAndMapsHTTPStatus(t *testing.T) {
	notFound := Define(40401, "USER_NOT_FOUND", "user not found", http.StatusNotFound)
	catalog, err := NewCatalog(notFound)
	if err != nil {
		t.Fatal(err)
	}
	converted := catalog.FromCode(40401, "user 123 not found")
	if converted.Code() != 40401 || converted.Message() != "user 123 not found" || converted.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("converted = %+v", converted)
	}
	unknown := catalog.FromCode(49999, "downstream business error")
	if unknown.Code() != 49999 || unknown.HTTPStatus() != http.StatusInternalServerError {
		t.Fatalf("unknown = %+v", unknown)
	}
	if _, err := NewCatalog(notFound, Define(40401, "OTHER", "other", http.StatusBadRequest)); err == nil {
		t.Fatal("duplicate code accepted")
	}
	if _, err := NewCatalog(notFound, Define(40402, "USER_NOT_FOUND", "other", http.StatusBadRequest)); err == nil {
		t.Fatal("duplicate name accepted")
	}
}

func TestMergeCatalogsRejectsCrossModuleConflicts(t *testing.T) {
	shared := MustCatalog(Define(40401, "USER_NOT_FOUND", "user not found", http.StatusNotFound))
	local := MustCatalog(Define(40402, "ORDER_NOT_FOUND", "order not found", http.StatusNotFound))
	merged, err := MergeCatalogs(shared, local)
	if err != nil || len(merged.Definitions()) != 2 {
		t.Fatalf("MergeCatalogs() = %+v, %v", merged, err)
	}
	conflict := MustCatalog(Define(40401, "OTHER_NOT_FOUND", "other not found", http.StatusNotFound))
	if _, err := MergeCatalogs(shared, conflict); err == nil || !strings.Contains(err.Error(), "duplicate code") {
		t.Fatalf("cross-module duplicate error = %v", err)
	}
}
