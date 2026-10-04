package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trb1maker/subscriptions/services/gateway/internal/app"
	"github.com/trb1maker/subscriptions/services/gateway/internal/domain"
)

func TestAcceptPaymentWebhook(t *testing.T) {
	t.Parallel()

	require.NoError(t, app.AcceptPaymentWebhook("secret", "secret"))
	require.ErrorIs(t, app.AcceptPaymentWebhook("nope", "secret"), domain.ErrInvalidWebhookKey)
	require.ErrorIs(t, app.AcceptPaymentWebhook("", "secret"), domain.ErrInvalidWebhookKey)
	require.ErrorIs(t, app.AcceptPaymentWebhook("secret", ""), domain.ErrInvalidWebhookKey)
	require.ErrorIs(t, app.AcceptPaymentWebhook("secret-long", "secret"), domain.ErrInvalidWebhookKey)
}
