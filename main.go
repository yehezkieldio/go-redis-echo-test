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
	"golang.org/x/sync/singleflight"
)

var (
	ctx     = context.Background()
	logger  zerolog.Logger
	rdb     *goredislib.Client
	rs      *redsync.Redsync
	sfGroup singleflight.Group
)

func initRedis() {
	rdb = goredislib.NewClient(&goredislib.Options{
		Addr:     "dragonfly:6379",
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

	// port := flag.Int("port", 3000, "Port to run the server on")
	// flag.Parse()

	port := os.Getenv("PORT")

	e := echo.New()
	e.Use(middleware.Gzip())
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
		users, err, _ := sfGroup.Do("users", func() (interface{}, error) {
			return rdb.Keys(ctx, "user:*").Result()
		})
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, users)
	})

	e.GET("/user/:username", func(c echo.Context) error {
		username := c.Param("username")
		key := "user:" + username

		user, err, _ := sfGroup.Do(key, func() (interface{}, error) {
			return rdb.Get(ctx, key).Result()
		})
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		if user == nil {
			return c.String(http.StatusNotFound, "User not found")
		}

		return c.JSON(http.StatusOK, user)
	})

	e.POST("/user", func(c echo.Context) error {
		logger.Info().Msg("Aquiring lock for user creation")
		mutex := rs.NewMutex("create_user_" + c.Param("username"))
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
		exists, err := rdb.Exists(ctx, key).Result()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}
		if exists > 0 {
			return c.String(http.StatusConflict, "User already exists")
		}

		_, err = rdb.TxPipelined(ctx, func(pipe goredislib.Pipeliner) error {
			pipe.Set(ctx, key, req.Username, 2*time.Minute)
			return nil
		})
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.String(http.StatusCreated, "User created")
	})

	e.PUT("/user/:username", func(c echo.Context) error {
		logger.Info().Msg("Aquiring lock for user update")
		mutex := rs.NewMutex("update_user_" + c.Param("username"))
		if err := mutex.Lock(); err != nil {
			logger.Error().Msg(err.Error())
			return c.String(http.StatusInternalServerError, err.Error())
		}
		defer mutex.Unlock()
		logger.Info().Msg("Released lock for user update")

		username := c.Param("username")

		key := "user:" + username

		_, err := rdb.Get(ctx, key).Result()
		if err == goredislib.Nil {
			return c.String(http.StatusNotFound, "User not found")
		}

		req := new(CreateUserRequest)
		if err := c.Bind(req); err != nil {
			return c.String(http.StatusBadRequest, err.Error())
		}

		_, err = rdb.Set(ctx, key, req.Username, 0).Result()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.String(http.StatusOK, "User updated")
	})

	e.DELETE("/user/:username", func(c echo.Context) error {
		logger.Info().Msg("Aquiring lock for user deletion")
		mutex := rs.NewMutex("delete_user_" + c.Param("username"))
		if err := mutex.Lock(); err != nil {
			logger.Error().Msg(err.Error())
			return c.String(http.StatusInternalServerError, err.Error())
		}
		defer mutex.Unlock()
		logger.Info().Msg("Released lock for user deletion")

		username := c.Param("username")

		key := "user:" + username

		_, err := rdb.Get(ctx, key).Result()
		if err == goredislib.Nil {
			return c.String(http.StatusNotFound, "User not found")
		}

		_, err = rdb.Del(ctx, key).Result()
		if err != nil {
			return c.String(http.StatusInternalServerError, err.Error())
		}

		return c.String(http.StatusOK, "User deleted")
	})

	e.HideBanner = true

	logger.Fatal().Msg(e.Start(":" + port).Error())

}
