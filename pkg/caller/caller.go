package caller

import (
	"context"

	"google.golang.org/grpc/metadata"

	"github.com/trb1maker/subscriptions/pkg/logger"
)

const (
	// MetadataSubject — идентификатор субъекта во входящем и исходящем gRPC.
	MetadataSubject = "caller-subject"
	// MetadataKind — вид субъекта строкой, без проверки доменных правил.
	MetadataKind = "caller-kind"
	// MetadataRole — одна роль; заголовок повторяется для каждой роли.
	MetadataRole = "caller-role"
	// MetadataRequestID — идентификатор внешнего запроса.
	MetadataRequestID = "request-id"
)

type callerKey struct{}

// Caller — транспортные сведения о вызывающем. Смысл kind и roles задаёт сервис на своей границе.
type Caller struct {
	SubjectID string
	Kind      string
	Roles     []string
}

// NewContext кладёт вызывающего в context. Пустой SubjectID не записывается.
func NewContext(ctx context.Context, c Caller) context.Context {
	if c.SubjectID == "" {
		return ctx
	}

	c.Roles = append([]string(nil), c.Roles...)

	return context.WithValue(ctx, callerKey{}, c)
}

// FromContext возвращает копию вызывающего.
func FromContext(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	if !ok {
		return Caller{}, false
	}

	c.Roles = append([]string(nil), c.Roles...)

	return c, true
}

// AppendOutgoing дописывает request id и вызывающего в исходящую metadata.
func AppendOutgoing(ctx context.Context) context.Context {
	pairs := make([]string, 0)
	if id := logger.RequestID(ctx); id != "" {
		pairs = append(pairs, MetadataRequestID, id)
	}

	if c, ok := FromContext(ctx); ok {
		pairs = append(pairs, MetadataSubject, c.SubjectID, MetadataKind, c.Kind)
		for _, role := range c.Roles {
			pairs = append(pairs, MetadataRole, role)
		}
	}

	if len(pairs) == 0 {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// FromIncoming переносит request id и вызывающего из входящей metadata в context.
func FromIncoming(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}

	if ids := md.Get(MetadataRequestID); len(ids) > 0 && ids[0] != "" {
		ctx = logger.WithRequestID(ctx, ids[0])
	}

	subjects := md.Get(MetadataSubject)
	if len(subjects) == 0 || subjects[0] == "" {
		return ctx
	}

	return NewContext(ctx, Caller{
		SubjectID: subjects[0],
		Kind:      first(md.Get(MetadataKind)),
		Roles:     md.Get(MetadataRole),
	})
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}

	return values[0]
}
