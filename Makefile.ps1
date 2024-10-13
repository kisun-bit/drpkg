$PROTO_BASE_DIR = "grpc\proto"
$protoDirs = Get-ChildItem -Path $PROTO_BASE_DIR -Directory

foreach ($dir in $protoDirs) {
    if (Test-Path $dir.FullName) {
        $relativePath = $dir.FullName.Substring($PWD.Path.Length + 1)  # 获取相对路径
        $cmd = "protoc --proto_path=. --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative $relativePath\*.proto"
        Write-Host $cmd
        Invoke-Expression $cmd
        Write-Host "Generated Go code for $relativePath"
    }
}

# Clean up generated .pb.go files
# Get-ChildItem -Path $PROTO_BASE_DIR -Recurse -Filter "*.pb.go" | Remove-Item -Force
