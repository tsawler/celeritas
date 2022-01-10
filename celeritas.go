package celeritas

import (
	"fmt"
	"github.com/tsawler/celeritas/filesystems/miniofilesystem"
	"github.com/tsawler/celeritas/filesystems/s3filesystem"
	"github.com/tsawler/celeritas/filesystems/sftpfilesystem"
	"github.com/tsawler/celeritas/filesystems/webdavfilesystem"
	"log"
	"net"
	"net/rpc"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/alexedwards/scs/v2"
	"github.com/dgraph-io/badger/v3"
	"github.com/go-chi/chi/v5"
	"github.com/gomodule/redigo/redis"
	"github.com/joho/godotenv"
	"github.com/robfig/cron/v3"
	"github.com/tsawler/celeritas/cache"
	"github.com/tsawler/celeritas/mailer"
	"github.com/tsawler/celeritas/render"
	"github.com/tsawler/celeritas/session"
)

const version = "1.0.0"

var myRedisCache *cache.RedisCache
var myBadgerCache *cache.BadgerCache
var redisPool *redis.Pool
var badgerConn *badger.DB
var maintenanceMode bool

// Celeritas is the overall type for the Celeritas package. Members that are exported in this type
// are available to any application that uses it.
type Celeritas struct {
	AppName       string
	Debug         bool
	Version       string
	ErrorLog      *log.Logger
	InfoLog       *log.Logger
	RootPath      string
	Routes        *chi.Mux
	Render        *render.Render
	Session       *scs.SessionManager
	DB            Database
	JetViews      *jet.Set
	config        config
	EncryptionKey string
	Cache         cache.Cache
	Scheduler     *cron.Cron
	Mail          mailer.Mail
	Server        Server
	FileSystems   map[string]interface{}
	S3            s3filesystem.S3
	SFTP          sftpfilesystem.SFTP
	WebDAV        webdavfilesystem.WebDAV
	Minio         miniofilesystem.Minio
}

type Server struct {
	ServerName string
	Port       string
	Secure     bool
	URL        string
}

type config struct {
	port        string
	renderer    string
	cookie      cookieConfig
	sessionType string
	database    databaseConfig
	redis       redisConfig
	uploads     uploadConfig
}

type uploadConfig struct {
	allowedMimeTypes []string
	maxUploadSize    int64
}

// New reads the .env file, creates our application config, populates the Celeritas type with settings
// based on .env values, and creates necessary folders and files if they don't exist
func (c *Celeritas) New(rootPath string) error {
	pathConfig := initPaths{
		rootPath:    rootPath,
		folderNames: []string{"handlers", "migrations", "views", "mail", "data", "public", "tmp", "logs", "middleware", "screenshots"},
	}

	err := c.Init(pathConfig)
	if err != nil {
		return err
	}

	err = c.checkDotEnv(rootPath)
	if err != nil {
		return err
	}

	// read .env
	err = godotenv.Load(rootPath + "/.env")
	if err != nil {
		return err
	}

	// create loggers
	infoLog, errorLog := c.startLoggers()

	// connect to database
	if os.Getenv("DATABASE_TYPE") != "" {
		db, err := c.OpenDB(os.Getenv("DATABASE_TYPE"), c.BuildDSN())
		if err != nil {
			errorLog.Println(err)
			os.Exit(1)
		}
		c.DB = Database{
			DataType: os.Getenv("DATABASE_TYPE"),
			Pool:     db,
		}
	}

	scheduler := cron.New()
	c.Scheduler = scheduler

	if os.Getenv("CACHE") == "redis" || os.Getenv("SESSION_TYPE") == "redis" {
		myRedisCache = c.createClientRedisCache()
		c.Cache = myRedisCache
		redisPool = myRedisCache.Conn
	}

	if os.Getenv("CACHE") == "badger" {
		myBadgerCache = c.createClientBadgerCache()
		c.Cache = myBadgerCache
		badgerConn = myBadgerCache.Conn

		_, err = c.Scheduler.AddFunc("@daily", func() {
			_ = myBadgerCache.Conn.RunValueLogGC(0.7)
		})
		if err != nil {
			return err
		}
	}

	c.InfoLog = infoLog
	c.ErrorLog = errorLog
	c.Debug, _ = strconv.ParseBool(os.Getenv("DEBUG"))
	c.Version = version
	c.RootPath = rootPath
	c.Mail = c.createMailer()
	c.Routes = c.routes().(*chi.Mux)

	// file uploads
	exploded := strings.Split(os.Getenv("ALLOWED_FILETYPES"), ",")
	var mimeTypes []string
	for _, m := range exploded {
		mimeTypes = append(mimeTypes, m)
	}

	var maxUploadSize int64
	if max, err := strconv.Atoi(os.Getenv("MAX_UPLOAD_SIZE")); err != nil {
		maxUploadSize = 10 << 20
	} else {
		maxUploadSize = int64(max)
	}

	c.config = config{
		port:     os.Getenv("PORT"),
		renderer: os.Getenv("RENDERER"),
		cookie: cookieConfig{
			name:     os.Getenv("COOKIE_NAME"),
			lifetime: os.Getenv("COOKIE_LIFETIME"),
			persist:  os.Getenv("COOKIE_PERSISTS"),
			secure:   os.Getenv("COOKIE_SECURE"),
			domain:   os.Getenv("COOKIE_DOMAIN"),
		},
		sessionType: os.Getenv("SESSION_TYPE"),
		database: databaseConfig{
			database: os.Getenv("DATABASE_TYPE"),
			dsn:      c.BuildDSN(),
		},
		redis: redisConfig{
			host:     os.Getenv("REDIS_HOST"),
			password: os.Getenv("REDIS_PASSWORD"),
			prefix:   os.Getenv("REDIS_PREFIX"),
		},
		uploads: uploadConfig{
			maxUploadSize:    maxUploadSize,
			allowedMimeTypes: mimeTypes,
		},
	}

	secure := true
	if strings.ToLower(os.Getenv("SECURE")) == "false" {
		secure = false
	}

	c.Server = Server{
		ServerName: os.Getenv("SERVER_NAME"),
		Port:       os.Getenv("PORT"),
		Secure:     secure,
		URL:        os.Getenv("APP_URL"),
	}

	// create session

	sess := session.Session{
		CookieLifetime: c.config.cookie.lifetime,
		CookiePersist:  c.config.cookie.persist,
		CookieName:     c.config.cookie.name,
		SessionType:    c.config.sessionType,
		CookieDomain:   c.config.cookie.domain,
	}

	switch c.config.sessionType {
	case "redis":
		sess.RedisPool = myRedisCache.Conn
	case "mysql", "postgres", "mariadb", "postgresql":
		sess.DBPool = c.DB.Pool
	}

	c.Session = sess.InitSession()
	c.EncryptionKey = os.Getenv("KEY")

	if c.Debug {
		var views = jet.NewSet(
			jet.NewOSFileSystemLoader(fmt.Sprintf("%s/views", rootPath)),
			jet.InDevelopmentMode(),
		)
		c.JetViews = views
	} else {
		var views = jet.NewSet(
			jet.NewOSFileSystemLoader(fmt.Sprintf("%s/views", rootPath)),
		)
		c.JetViews = views
	}

	c.createRenderer()
	c.FileSystems = c.createFileSystems()
	go c.Mail.ListenForMail()

	return nil
}

// Init creates necessary folders for our Celeritas application
func (c *Celeritas) Init(p initPaths) error {
	root := p.rootPath
	for _, path := range p.folderNames {
		// create folder if it doesn't exist
		err := c.CreateDirIfNotExist(root + "/" + path)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Celeritas) checkDotEnv(path string) error {
	err := c.CreateFileIfNotExists(fmt.Sprintf("%s/.env", path))
	if err != nil {
		return err
	}
	return nil
}

func (c *Celeritas) startLoggers() (*log.Logger, *log.Logger) {
	var infoLog *log.Logger
	var errorLog *log.Logger

	infoLog = log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog = log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	return infoLog, errorLog
}

func (c *Celeritas) createRenderer() {
	myRenderer := render.Render{
		Renderer: c.config.renderer,
		RootPath: c.RootPath,
		Port:     c.config.port,
		JetViews: c.JetViews,
		Session:  c.Session,
	}
	c.Render = &myRenderer
}

func (c *Celeritas) createMailer() mailer.Mail {
	port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
	m := mailer.Mail{
		Domain:      os.Getenv("MAIL_DOMAIN"),
		Templates:   c.RootPath + "/mail",
		Host:        os.Getenv("SMTP_HOST"),
		Port:        port,
		Username:    os.Getenv("SMTP_USERNAME"),
		Password:    os.Getenv("SMTP_PASSWORD"),
		Encryption:  os.Getenv("SMTP_ENCRYPTION"),
		FromName:    os.Getenv("FROM_NAME"),
		FromAddress: os.Getenv("FROM_ADDRESS"),
		Jobs:        make(chan mailer.Message, 20),
		Results:     make(chan mailer.Result, 20),
		API:         os.Getenv("MAILER_API"),
		APIKey:      os.Getenv("MAILER_KEY"),
		APIUrl:      os.Getenv("MAILER_URL"),
	}
	return m
}

func (c *Celeritas) createClientRedisCache() *cache.RedisCache {
	cacheClient := cache.RedisCache{
		Conn:   c.createRedisPool(),
		Prefix: c.config.redis.prefix,
	}
	return &cacheClient
}

func (c *Celeritas) createClientBadgerCache() *cache.BadgerCache {
	cacheClient := cache.BadgerCache{
		Conn: c.createBadgerConn(),
	}
	return &cacheClient
}

func (c *Celeritas) createRedisPool() *redis.Pool {
	return &redis.Pool{
		MaxIdle:     50,
		MaxActive:   10000,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp",
				c.config.redis.host,
				redis.DialPassword(c.config.redis.password))
		},

		TestOnBorrow: func(conn redis.Conn, t time.Time) error {
			_, err := conn.Do("PING")
			return err
		},
	}
}

func (c *Celeritas) createBadgerConn() *badger.DB {
	db, err := badger.Open(badger.DefaultOptions(c.RootPath + "/tmp/badger"))
	if err != nil {
		return nil
	}
	return db
}

// BuildDSN builds the datasource name for our database, and returns it as a string
func (c *Celeritas) BuildDSN() string {
	var dsn string

	switch os.Getenv("DATABASE_TYPE") {
	case "postgres", "postgresql":
		dsn = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s timezone=UTC connect_timeout=5",
			os.Getenv("DATABASE_HOST"),
			os.Getenv("DATABASE_PORT"),
			os.Getenv("DATABASE_USER"),
			os.Getenv("DATABASE_NAME"),
			os.Getenv("DATABASE_SSL_MODE"))

		// we check to see if a database password has been supplied, since including "password=" with nothing
		// after it sometimes causes postgres to fail to allow a connection.
		if os.Getenv("DATABASE_PASS") != "" {
			dsn = fmt.Sprintf("%s password=%s", dsn, os.Getenv("DATABASE_PASS"))
		}

	case "mysql", "mariadb":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?collation=utf8_unicode_ci&timeout=5s&parseTime=true&tls=%s&readTimeout=5s",
			os.Getenv("DATABASE_USER"),
			os.Getenv("DATABASE_PASS"),
			os.Getenv("DATABASE_HOST"),
			os.Getenv("DATABASE_PORT"),
			os.Getenv("DATABASE_NAME"),
			os.Getenv("DATABASE_SSL_MODE"))

	default:

	}

	return dsn
}

func (c *Celeritas) createFileSystems() map[string]interface{} {
	fileSystems := make(map[string]interface{})

	if os.Getenv("S3_KEY") != "" {
		s3 := s3filesystem.S3{
			Key:      os.Getenv("S3_KEY"),
			Secret:   os.Getenv("S3_SECRET"),
			Region:   os.Getenv("S3_REGION"),
			Endpoint: os.Getenv("S3_ENDPOINT"),
			Bucket:   os.Getenv("S3_BUCKET"),
		}
		fileSystems["S3"] = s3
		c.S3 = s3
	}

	if os.Getenv("MINIO_SECRET") != "" {
		useSSL := false
		if strings.ToLower(os.Getenv("MINIO_USESSL")) == "true" {
			useSSL = true
		}

		minio := miniofilesystem.Minio{
			Endpoint: os.Getenv("MINIO_ENDPOINT"),
			Key:      os.Getenv("MINIO_KEY"),
			Secret:   os.Getenv("MINIO_SECRET"),
			UseSSL:   useSSL,
			Region:   os.Getenv("MINIO_REGION"),
			Bucket:   os.Getenv("MINIO_BUCKET"),
		}
		fileSystems["MINIO"] = minio
		c.Minio = minio
	}

	if os.Getenv("SFTP_HOST") != "" {
		sftp := sftpfilesystem.SFTP{
			Host: os.Getenv("SFTP_HOST"),
			User: os.Getenv("SFTP_USER"),
			Pass: os.Getenv("SFTP_PASS"),
			Port: os.Getenv("SFTP_PORT"),
		}
		fileSystems["SFTP"] = sftp
		c.SFTP = sftp
	}

	if os.Getenv("WEBDAV_HOST") != "" {
		webDav := webdavfilesystem.WebDAV{
			Host: os.Getenv("WEBDAV_HOST"),
			User: os.Getenv("WEBDAV_USER"),
			Pass: os.Getenv("WEBDAV_PASS"),
		}
		fileSystems["WEBDAV"] = webDav
		c.WebDAV = webDav
	}

	return fileSystems
}

type RPCServer struct{}

func (r *RPCServer) MaintenanceMode(inMaintenanceMode bool, resp *string) error {
	if inMaintenanceMode {
		maintenanceMode = true
		*resp = "Server in maintenance mode"
	} else {
		maintenanceMode = false
		*resp = "Server live!"
	}
	return nil
}

func (c *Celeritas) listenRPC() {
	// if nothing specified for rpc port, don't start
	if os.Getenv("RPC_PORT") != "" {
		c.InfoLog.Println("Starting RPC server on port", os.Getenv("RPC_PORT"))
		err := rpc.Register(new(RPCServer))
		if err != nil {
			c.ErrorLog.Println(err)
			return
		}
		listen, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("RPC_PORT"))
		if err != nil {
			c.ErrorLog.Println(err)
			return
		}
		for {
			rpcConn, err := listen.Accept()
			if err != nil {
				continue
			}
			go rpc.ServeConn(rpcConn)
		}

	}
}
