package webserver

import (
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/controllers"
	"MySportWeb/internal/pkg/middlewares"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"net/http"
)

type WebServer struct {
	Router *gin.Engine
}

func AppHandler(App *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("App", App)
		c.Next()
	}
}

func RunHttp(listenAddr string, App *app.App) error {

	httpRouter := gin.Default()

	//	httpRouter.LoadHTMLGlob("templates/*")
	httpRouter.Use(static.Serve("/static", static.LocalFile("./static", true)))

	httpRouter.Use(AppHandler(App))
	httpRouter.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title":    "MySportWeb",
			"vapidkey": App.Notifications.PubKey,
		})
	})

	httpRouter.GET("/signup", func(c *gin.Context) {
		c.HTML(http.StatusOK, "signup.tmpl", gin.H{
			"title": "SwitchDB Signup",
		})

	})
	httpRouter.StaticFile("/service-worker.js", "./resources/service-worker.js")

	apiv1 := httpRouter.Group("/api/v1")
	{
		apiv1.GET("/", middlewares.IsAuthorized(), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"message": "pong",
			})
		})
	}

	apiv1.GET("/healthcheck", controllers.HealthCheck)
	// Users
	apiv1.POST("/user/login", controllers.Login)
	apiv1.POST("/user/register", controllers.Register)
	apiv1.GET("/user/logout", middlewares.IsAuthorized(), controllers.Logout)

	// Activities
	apiv1.POST("/activity", middlewares.IsAuthorized(), controllers.UploadActivity)
	apiv1.GET("/activity/list", middlewares.IsAuthorized(), controllers.ListActivities)
	apiv1.GET("/activity/:id", middlewares.IsAuthorized(), controllers.GetActivity)
	apiv1.DELETE("/activity/:id", middlewares.IsAuthorized(), controllers.DeleteActivity)
	apiv1.POST("/activity/:id", middlewares.IsAuthorized(), controllers.UpdateActivity)

	// Start and run the server
	err := httpRouter.Run(listenAddr)
	return err

}
