package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	goredislib "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var (
	ctx    = context.Background()
	logger zerolog.Logger
	rdb    *goredislib.Client
	rs     *redsync.Redsync
)

func initRedis() {
	rdb = goredislib.NewClient(&goredislib.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		logger.Fatal().Msg(err.Error())
		os.Exit(1)
	}

	logger.Info().Msg("Connected to Redis")

	pool := goredis.NewPool(rdb)

	rs = redsync.New(pool)

	logger.Info().Msg("Connected to Redsync")
}

func initLogger() {
	logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339},
	).Level(zerolog.TraceLevel).With().Timestamp().Logger().Hook()
}

type CreateUserRequest struct {
	Username string `json:"username"`
}

func main() {
	initLogger()
	initRedis()

	e := echo.New()
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info().
				Str("URI", v.URI).
				Int("status", v.Status).
				Str("latency", v.Latency.String()).
				Msg("request")

			return nil
		},
	}))

	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Hello, World!")
	})

	e.GET("/user", func(c echo.Context) error {
		users, err := rdb.Keys(ctx, "user:*").Result()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, users)
	})

	e.POST("/user", func(c echo.Context) error {
		logger.Info().Msg("Aquiring lock for user creation")
		mutex := rs.NewMutex("create_user")
		if err := mutex.Lock(); err != nil {
			logger.Error().Msg(err.Error())
			return c.String(http.StatusInternalServerError, err.Error())
		}
		defer mutex.Unlock()
		logger.Info().Msg("Released lock for user creation")

		req := new(CreateUserRequest)
		if err := c.Bind(req); err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}

		key := "user:" + req.Username
		_, err := rdb.Get(ctx, key).Result()
		if err != goredislib.Nil {
			return c.String(http.StatusConflict, "User already exists")
		}

		_, err = rdb.Set(ctx, key, req.Username, 0).Result()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.String(http.StatusCreated, "User created")
	})

	e.HideBanner = true

	logger.Fatal().Msg(e.Start(":3000").Error())
}
