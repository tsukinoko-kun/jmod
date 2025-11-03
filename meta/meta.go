package meta

import "os"

var Version uint = 1

var pwd string

func Pwd() string {
	if pwd != "" {
		return pwd
	}
	var err error
	pwd, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	return pwd
}
