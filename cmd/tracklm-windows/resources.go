package main

//go:generate go run github.com/akavel/rsrc@latest -arch amd64 -manifest tracklm-windows.exe.manifest -ico ../../assets/app-icon.ico -o rsrc_windows_amd64.syso
//go:generate go run github.com/akavel/rsrc@latest -arch arm64 -manifest tracklm-windows.exe.manifest -ico ../../assets/app-icon.ico -o rsrc_windows_arm64.syso
