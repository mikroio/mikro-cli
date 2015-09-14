package main

import (
  "archive/tar"
  "path/filepath"
  "fmt"
  "io"
  "os"
)

func Untar(in io.ReadCloser, dst string) error {
  tarBallReader := tar.NewReader(in)

  // Extracting tarred files
  for {
    header, err := tarBallReader.Next()
    if err != nil {
      if err == io.EOF {
        return nil
      }
      return err
    }

    // get the individual filename and extract to the current directory
    filename := header.Name

    switch header.Typeflag {
    case tar.TypeDir:
      // handle directory
      //fmt.Println("Creating directory :", filename)
      err = os.MkdirAll(filepath.Join(dst, filename), os.FileMode(header.Mode))
      if err != nil {
        return err
      }
    case tar.TypeReg:
      // handle normal file
      //fmt.Println("Untarring :", filename)
      writer, err := os.Create(filepath.Join(dst, filename))
      if err != nil {
        return err
      }

      io.Copy(writer, tarBallReader)
      err = os.Chmod(filepath.Join(dst, filename), os.FileMode(header.Mode))
      if err != nil {
        return err
      }
      writer.Close()

    default:
      fmt.Printf("Unable to untar type : %c in file %s", header.Typeflag, filename)
    }
  }
}
