package util

import (
	"os/exec"
	"fmt"
	"log"
	"io/ioutil"
	"bytes"
	"errors"
	"math/rand"
	"time"
	"encoding/json"
	//"golang.org/x/sys/unix"
)

/****Standard Data Structures****/
type KeyPair struct {
	PrivateKey	string
	PublicKey	string
}


/****Basic Linux Functions****/

/**
 * Remove directories or files
 * @param  ...string	directories	The directories and files to delete
 */
func Rm(directories ...string){
	for _, directory := range directories {
		if conf.Verbose {
			fmt.Printf("Removing  %s...",directory)
		}
		
		cmd := exec.Command("bash","-c",fmt.Sprintf("rm -rf %s",directory))
		cmd.Run()
		if conf.Verbose {
			fmt.Printf("done\n")
		}
		
	}
}

/**
 * Create a directory
 * @param  string	directory	The directory to create
 */
func Mkdir(directory string){
	if conf.Verbose {
		fmt.Printf("Creating directory %s\n",directory)
	}
	
	cmd := exec.Command("mkdir","-p",directory)
	cmd.Run()
}

/**
 * Copy a file
 * @param  string	src		The source of the file
 * @param  string	dest	The destination for the file
 */
func Cp(src string, dest string){
	if conf.Verbose {
		fmt.Printf("Copying %s to %s\n",src,dest)
	}
	
	cmd := exec.Command("bash","-c",fmt.Sprintf("cp %s %s",src,dest))
	cmd.Run()
}

/**
 * Copy a directory
 * @param  string	src		The source of the directory
 * @param  string	dest	The destination for the directory
 */
func Cpr(src string,dest string){
	if conf.Verbose {
		fmt.Printf("Copying %s to %s\n",src,dest)
	}

	cmd := exec.Command("cp","-r",src,dest)
	cmd.Run()
}

/**
 * Write data to a file
 * @param  string	path	The file to write to
 * @param  string	data	The data to write to the file
 */
func Write(path string,data string){
	if conf.Verbose {
		fmt.Printf("Writing to file %s...",path)
	}
	
	err := ioutil.WriteFile(path,[]byte(data),0664)
	CheckFatal(err)
	if conf.Verbose {
		fmt.Printf("done\n")
	}
	
}

/**
 * Lists the contents of a directory recursively
 * @param  string	_dir 	The directory
 * @return []string			List of the contents of the directory
 */
func Lsr(_dir string) []string {
	dir := _dir
	if(dir[len(dir) - 1:] != "/"){
		dir += "/"
	}
	out := []string{}
	files, err := ioutil.ReadDir(dir)
	CheckFatal(err)
	for _, f := range files {
        if(f.IsDir()){
        	out = append(out,Lsr(fmt.Sprintf("%s%s/",dir,f.Name())) ...)
        }else{
        	out = append(out,fmt.Sprintf("%s%s",dir,f.Name()))
        }
    }
    return out
}

/**
 * List directories in order of construction
 */
func LsDir(_dir string) []string {
	dir := _dir
	if(dir[len(dir) - 1:] != "/"){
		dir += "/"
	}
	out := []string{}
	files, err := ioutil.ReadDir(dir)
	CheckFatal(err)
	for _, f := range files {
        if(f.IsDir()){
        	out = append(out,fmt.Sprintf("%s%s/",dir,f.Name()))
        	out = append(out,LsDir(fmt.Sprintf("%s%s/",dir,f.Name())) ...)
        }
    }
    return out
}

/**
 * Check and exit if error
 * @param  error	err The Error
 */
func CheckFatal(err error){
	if err != nil {
		panic(err)
	}
}


/**
 * Check and log if error
 * @param  Error 	err 	The Error to check
 */
func Check(err error){
	if err != nil {
		log.Fatal(err)
	}
}

/**
 * Check if there was an error, and if so, print out
 * custom error message
 * @param  error    err       The Potential Error
 * @param  string   msg       The message to show
 */
func CheckAndPrint(err error,msg string){
	if err != nil {
		fmt.Println(msg)
		panic(err)
	}
}

/**
 * Combine an Array with \n as the delimiter
 */
func CombineConfig(entries []string) string {
	out := ""
	for _,entry := range entries {
		out += fmt.Sprintf("%s\n",entry)
	}
	return out
}

/**
 * Execute _cmd in bash then return the result
 * @param  string 	_cmd 	The command to execute
 * @return string 			The result of execution
 */
func BashExec(_cmd string) string {
	if conf.Verbose {
		fmt.Printf("Execuing : %s\n",_cmd)
	}
	
	cmd := exec.Command("bash","-c",_cmd)

	var resultsRaw bytes.Buffer

	cmd.Stdout = &resultsRaw
	cmd.Start()
	cmd.Wait()

	return resultsRaw.String()
}

func IntArrRemove(op []int,index int) []int {
	return append(op[:index],op[index+1:]...)
}

func IntArrFill(size int, f func(int) int) []int {
	out := make([]int,size)
	for i := 0; i < size; i++ {
		out[i] = f(i)
	}
	return out
}


func Distribute(nodes []string,dist []int) ([][]string,error){
	if len(nodes) < 2 {
		return nil,errors.New("Cannot distribute a series smaller than 1")
	}
	for _,d := range dist{
		if d >= len(nodes){
			return nil,errors.New("Cannot distribute among more nodes than those that are given")
		}
	}
	s1 := rand.NewSource(time.Now().UnixNano())
    	r1 := rand.New(s1)

	out := [][]string{}
	for i, _ := range nodes {
		conns := []string{}
		for j := 0; j < dist[i]; j++ {
			newConnIndex := r1.Intn(len(nodes))
			if newConnIndex == i {
				j--
				continue
			}
			newConn := nodes[newConnIndex]
			unique := true
			for _, conn := range conns{
				if newConn == conn {
					unique = false
					break
				}
			}
			if !unique {
				j--
				continue
			}
			conns = append(conns,newConn)
		
		}
		out = append(out,conns)
	}
	return out,nil
}


func GetJSONNumber(data map[string]interface{},field string) (json.Number,error){
	rawValue,exists := data[field]
	if exists && rawValue != nil {
		switch rawValue.(type){
			case json.Number:
				value,valid := rawValue.(json.Number)
				if !valid {
					return "",errors.New("Invalid json number")
				}
				return value,nil
				
		}
	}
	return "",errors.New("Incorrect type for "+field+" given")
}


func GetJSONInt64(data map[string]interface{},field string) (int64,error){
	rawValue,exists := data[field]
	if exists && rawValue != nil {
		switch rawValue.(type){
			case json.Number:
				value,err := rawValue.(json.Number).Int64()
				if err != nil {
					return 0,err
				}
				return value,nil
				
		}
	}
	return 0,errors.New("Incorrect type for "+field+" given")
}

func GetJSONStringArr(data map[string]interface{},field string) ([]string,error){
	rawValue,exists := data[field]
	if exists && rawValue != nil {
		switch rawValue.(type){
			case []string:
				value,valid := rawValue.([]string)
				if !valid {
					return nil,errors.New("Invalid string array")
				}
				return value,nil
				
		}
	}
	return nil,errors.New("Incorrect type for "+field+" given")
}

func GetJSONString(data map[string]interface{},field string) (string,error){
	rawValue,exists := data[field]
	if exists && rawValue != nil {
		switch rawValue.(type){
			case string:
				value,valid := rawValue.(string)
				if !valid {
					return "",errors.New("Invalid string")
				}
				return value,nil
				
		}
	}
	return "",errors.New("Incorrect type for "+field+" given")
}

func GetJSONBool(data map[string]interface{},field string) (bool,error){
	rawValue,exists := data[field]
	if exists && rawValue != nil {
		switch rawValue.(type){
			case bool:
				value,valid := rawValue.(bool)
				if !valid {
					return false,errors.New("Invalid bool")
				}
				return value,nil
				
		}
	}
	return false,errors.New("Incorrect type for "+field+" given")
}