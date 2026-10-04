package redis

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/trb1maker/subscriptions/pkg/health"
)

const sentinelScheme = "sentinel://"

// New открывает клиент и проверяет, что Redis принимает запросы.
// sentinel://:password@host:26379,host2:26379/0?master=name идёт через Sentinel.
func New(ctx context.Context, rawURL string) (*goredis.Client, error) {
	client, err := open(rawURL)
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

func open(rawURL string) (*goredis.Client, error) {
	if strings.HasPrefix(rawURL, sentinelScheme) {
		options, err := parseSentinel(rawURL)
		if err != nil {
			return nil, err
		}

		return goredis.NewFailoverClient(options), nil
	}

	options, err := goredis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	return goredis.NewClient(options), nil
}

func parseSentinel(rawURL string) (*goredis.FailoverOptions, error) {
	rest := strings.TrimPrefix(rawURL, sentinelScheme)

	var userinfo string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		userinfo = rest[:at]
		rest = rest[at+1:]
	}

	hostPart, pathQuery, _ := strings.Cut(rest, "/")
	if hostPart == "" {
		return nil, fmt.Errorf("parse redis url: sentinel address required")
	}

	path, query, _ := strings.Cut(pathQuery, "?")
	values, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	master := values.Get("master")
	if master == "" {
		return nil, fmt.Errorf("parse redis url: master required")
	}

	db := 0
	if path != "" {
		db, err = strconv.Atoi(path)
		if err != nil {
			return nil, fmt.Errorf("parse redis url: %w", err)
		}
	}

	password, err := sentinelPassword(userinfo)
	if err != nil {
		return nil, err
	}

	return &goredis.FailoverOptions{
		MasterName:    master,
		SentinelAddrs: strings.Split(hostPart, ","),
		Password:      password,
		DB:            db,
	}, nil
}

func sentinelPassword(userinfo string) (string, error) {
	if userinfo == "" {
		return "", nil
	}

	_, password, found := strings.Cut(userinfo, ":")
	if !found {
		password = userinfo
	}

	decoded, err := url.QueryUnescape(password)
	if err != nil {
		return "", fmt.Errorf("parse redis url: %w", err)
	}

	return decoded, nil
}

// Check — проверка Redis для GET /health.
func Check(client *goredis.Client) health.Check {
	return health.Check{
		Name: "redis",
		Fn: func(ctx context.Context) error {
			return client.Ping(ctx).Err()
		},
	}
}
