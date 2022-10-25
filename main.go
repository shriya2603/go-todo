package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gofrs/uuid"
	"github.com/joho/godotenv"
	"github.com/thedevsaddam/renderer"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var render *renderer.Render
var db *gorm.DB

const (
	port string = ":9010"
)

type (
	// Act as model for database
	TodoModel struct {
		gorm.Model
		ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
		Title     string    `gorm:"not null;type:text" json:"title"`
		Completed bool      `gorm:"type:bool" json:"completed"`
	}

	// todo struct used by the project to send to fontend
	todo struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Completed bool   `json:"completed"`
	}

	DBConfig struct {
		Host     string
		Port     string
		Password string
		User     string
		DBName   string
		SSLMode  string
	}
)

func NewDBConnection(config DBConfig) error {
	dbURL := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s",
		config.Host, config.Port, config.User, config.Password, config.DBName,
	)

	database, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{AllowGlobalUpdate: true})
	if err != nil {
		log.Fatal(fmt.Sprintf("Failed to create connection with postgres %v", err))
		return err
	}

	database.AutoMigrate(&TodoModel{})
	db = database
	return nil
}

func loadDbConfig() (DBConfig, error) {
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("Error occurred while loading an env file ")
		return DBConfig{}, err
	}
	config := &DBConfig{
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		Password: os.Getenv("DB_PASS"),
		User:     os.Getenv("DB_USER"),
		SSLMode:  os.Getenv("DB_SSLMODE"),
		DBName:   os.Getenv("DB_DBNAME"),
	}
	return *config, err
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	render = renderer.New()
	databaseConfig, envErr := loadDbConfig()
	checkErr(envErr)
	fmt.Println("db config ", databaseConfig)

	err := NewDBConnection(databaseConfig)
	checkErr(err)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	err := render.Template(w, http.StatusOK, []string{"static/home.tpl"}, nil)
	checkErr(err)
}

func fetchTodo(w http.ResponseWriter, r *http.Request) {
	todos := []TodoModel{}
	//getting the todos from db
	if result := db.Find(&todos); result.Error != nil {
		render.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to fetch todo",
			"error":   result.Error,
		})
		return
	}
	todoList := []todo{}
	for _, t := range todos {
		todoList = append(todoList, todo{
			ID:        t.ID.String(),
			Title:     t.Title,
			Completed: t.Completed,
		})
	}

	// sending response
	render.JSON(w, http.StatusOK, renderer.M{
		"data": todoList,
	})
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	// get the created todo from request
	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		render.JSON(w, http.StatusProcessing, err)
		return
	}

	// validation
	if t.Title == "" {
		render.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "Title is required",
		})
		return
	}

	// Create a todo model
	todoM := TodoModel{
		Title:     t.Title,
		Completed: false,
	}

	if result := db.Create(&todoM); result.Error != nil {
		render.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to create todo",
			"data":    result.Error,
		})
		return
	}

	render.JSON(w, http.StatusCreated, renderer.M{
		"message": "todo created successfully",
		"todo_id": todoM.ID.String(),
	})

}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	if id == "" {
		render.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "id is not be empty",
		})
		return
	}

	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		render.JSON(w, http.StatusProcessing, err)
		return
	}

	if t.Title == "" {
		render.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Title is empty ",
		})
		return
	}

	if result := db.Exec("UPDATE todo_models SET title = ?, completed = ? WHERE id = ?", t.Title, t.Completed, id); result.Error != nil {
		render.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to update todo",
			"error":   result.Error,
		})
		return
	}

	render.JSON(w, http.StatusOK, renderer.M{
		"message": "updated the todo",
	})
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	if id == "" {
		render.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "id is invalid",
		})
		return
	}
	if result := db.Exec("DELETE FROM todo_models Where id = ?", id); result.Error != nil {
		render.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to delete todo",
			"error":   result.Error,
		})
		return
	}

	render.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo delete successfully ",
	})
}

func todoHandlers() http.Handler {
	rg := chi.NewRouter()
	rg.Group(func(r chi.Router) {
		r.Get("/", fetchTodo)
		r.Post("/", createTodo)
		r.Put("/{id}", updateTodo)
		r.Delete("/{id}", deleteTodo)
	})
	return rg
}

func main() {
	// stop the server gracefully
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Get("/", homeHandler)
	router.Mount("/todo", todoHandlers())

	server := &http.Server{
		Addr:         port,
		Handler:      router,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Listening on port ", port)
		if err := server.ListenAndServe(); err != nil {
			log.Printf("listen:%s\n", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down server ")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	server.Shutdown(ctx)
	defer cancel()
	log.Println("Server gracefully stopped")
}
