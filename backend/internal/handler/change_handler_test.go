package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireChangeOperatorUsesAuthenticatedUsernameInsteadOfRequestOperator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	requestBodies := []string{
		`{"approved_by":"request-user"}`,
		`{"rejected_by":"request-user"}`,
		`{"executed_by":"request-user"}`,
	}

	for _, body := range requestBodies {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/api/changes/1/transition", bytes.NewBufferString(body))
		context.Set("username", "authenticated-user")

		operator, ok := requireChangeOperator(context)

		if !ok {
			t.Fatal("requireChangeOperator() rejected an authenticated request")
		}
		if operator != "authenticated-user" {
			t.Fatalf("requireChangeOperator() = %q, want authenticated username %q", operator, "authenticated-user")
		}
	}
}

func TestChangeTransitionsRejectMissingAuthenticatedUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ChangeHandler{}
	transitions := []struct {
		name    string
		body    string
		handler func(*gin.Context)
	}{
		{name: "submit", handler: handler.Submit},
		{name: "approve", body: `{"approved_by":"request-user"}`, handler: handler.Approve},
		{name: "reject", body: `{"rejected_by":"request-user"}`, handler: handler.Reject},
		{name: "execute", body: `{"executed_by":"request-user"}`, handler: handler.Execute},
		{name: "complete", handler: handler.Complete},
		{name: "rollback", handler: handler.Rollback},
		{name: "fail", handler: handler.Fail},
	}

	for _, transition := range transitions {
		t.Run(transition.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Params = gin.Params{{Key: "id", Value: "1"}}
			context.Request = httptest.NewRequest(http.MethodPost, "/api/changes/1/"+transition.name, bytes.NewBufferString(transition.body))

			transition.handler(context)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; response body: %s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}
