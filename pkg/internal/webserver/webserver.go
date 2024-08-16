package webserver

import (
	"MySportWeb/pkg/internal/app"
	"MySportWeb/pkg/internal/controllers"
	"MySportWeb/pkg/internal/middlewares"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"net/http"
)

type WebServer struct {
	Router *gin.Engine
}

func AppHandler(App *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("storeApp", App)
		c.Next()
	}
}

func RunHttp(listenAddr string, storeApp *app.App) error {

	httpRouter := gin.Default()

	httpRouter.LoadHTMLGlob("templates/*")
	httpRouter.Use(static.Serve("/static", static.LocalFile("./static", true)))

	httpRouter.Use(AppHandler(storeApp))
	httpRouter.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title":    "SwitchDB Web Interface",
			"vapidkey": storeApp.Notifications.PubKey,
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
	apiv1.GET("/user/refreshtoken", middlewares.IsAuthorized(), controllers.RefreshToken)
	apiv1.GET("/user", middlewares.IsAuthorized(), controllers.GetUser)
	apiv1.POST("/user", middlewares.IsAuthorized(), controllers.UpdateUser)
	apiv1.GET("/whoami", middlewares.IsAuthorized(), controllers.WhoAmI)
	// Games
	apiv1.GET("/games", controllers.GetGames)
	apiv1.GET("/game/:gameID", middlewares.IsAuthorized(), controllers.GetGame)
	apiv1.GET("/getgameprices/gameNameID", middlewares.IsAuthorized(), controllers.GetGamePrices)
	apiv1.POST("/searchgame", middlewares.IsAuthorized(), controllers.SearchGame)
	apiv1.POST("/trackgame", middlewares.IsAuthorized(), controllers.AddGame)
	apiv1.POST("/untrackgame", middlewares.IsAuthorized(), controllers.UntrackGame)
	// Notification channels
	apiv1.POST("/channel", middlewares.IsAuthorized(), controllers.AddChannel)
	apiv1.POST("/channel/:channelID", middlewares.IsAuthorized(), controllers.UpdateChannel)
	apiv1.GET("/channel/:channelID", middlewares.IsAuthorized(), controllers.GetChannel)
	apiv1.DELETE("/channel/:channelID", middlewares.IsAuthorized(), controllers.DeleteChannel)
	apiv1.GET("/channel/:channelID/test", middlewares.IsAuthorized(), controllers.TestChannel)
	apiv1.GET("/channels", middlewares.IsAuthorized(), controllers.GetChannels)
	apiv1.GET("/getvapidkey", controllers.GetKey)

	// Admin
	apiv1.DELETE("/admin/user", middlewares.IsAdmin(), controllers.DeleteUser)
	apiv1.GET("/admin/users", middlewares.IsAdmin(), controllers.GetAllUsers)
	apiv1.GET("/admin/games", middlewares.IsAdmin(), controllers.GetAllGames)
	apiv1.PUT("/admin/user/:userID", middlewares.IsAdmin(), controllers.UpdateUserAdmin)

	// Start and run the server
	err := httpRouter.Run(listenAddr)
	return err

}
