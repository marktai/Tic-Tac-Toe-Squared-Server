package game

import (
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	// "gopkg.in/mgo.v2"
	// "gopkg.in/mgo.v2/bson"
	"errors"
	"log"
	"math/rand"
	"time"
)

var (
	db        *sql.DB
	closeChan chan bool
)

type dbgame struct {
	gameid       uint
	player0      uint
	player1      uint
	turn         uint
	box0         uint
	box1         uint
	box2         uint
	box3         uint
	box4         uint
	box5         uint
	box6         uint
	box7         uint
	box8         uint
	movehistory0 uint64
	movehistory1 uint64
	started      time.Time
	modified     time.Time
}

func getUniqueID() (uint, error) {

	rand.Seed(time.Now().Unix())

	collision := 1
	times := 0
	var id uint

	for collision != 0 {

		id = uint(rand.Int31n(65536))
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM games WHERE gameid=?)", id).Scan(&collision)
		if err != nil {
			return id, err
		}
		times++
		if times > 20 {
			return id, errors.New("Too many attempts to find a unique game ID")
		}
	}
	return id, nil
}

func (g *dbgame) game() *Game {

	var newGame Game

	newGame.GameID = g.gameid
	newGame.Players = [2]uint{g.player0, g.player1}

	comprBoard := [9]uint{g.box0, g.box1, g.box2, g.box3, g.box4, g.box5, g.box6, g.box7, g.box8}
	newGame.Board.Decompress(comprBoard)

	newGame.Turn = g.turn
	newGame.MoveHistory.Decompress(g.movehistory0, g.movehistory1)
	newGame.Started = g.started
	newGame.Modified = g.modified

	return &newGame
}

func (g *dbgame) update() (sql.Result, error) {
	err := db.Ping()
	if err != nil {
		return nil, err
	}

	updateGame, err := db.Prepare("UPDATE games SET turn=?, box0=?, box1=?, box2=?, box3=?, box4=?, box5=?, box6=?, box7=?, box8=?, movehistory0=?, movehistory1=?, modified=? WHERE gameid=?")

	if err != nil {
		return nil, err
	}

	return updateGame.Exec(g.turn, g.box0, g.box1, g.box2, g.box3, g.box4, g.box5, g.box6, g.box7, g.box8, g.movehistory0, g.movehistory1, g.modified, g.gameid)
}

func (g *dbgame) upload() (sql.Result, error) {
	err := db.Ping()
	if err != nil {
		return nil, err
	}

	addGame, err := db.Prepare("INSERT INTO games VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	return addGame.Exec(g.gameid, g.player0, g.player1, g.turn, g.box0, g.box1, g.box2, g.box3, g.box4, g.box5, g.box6, g.box7, g.box8, g.movehistory0, g.movehistory1, g.started, g.modified)
}

func Open() {
	closeChan = make(chan bool)
	var err error
	db, err = sql.Open("mysql",
		"root:@tcp(127.0.0.1:3306)/TT2")

	if err != nil {
		log.Fatal(err)
	}
	go func() {
		<-closeChan
		db.Close()
	}()
}

func Close() {
	closeChan <- true
}
