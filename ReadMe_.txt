启动S3存储的命令

-s3-bucket=test
-s3-endpoint=http://127.0.0.1:9000
-upload-dir=./data
-base-path=/files
-host=0.0.0.0
-port=8080

启动S3存储前 需要在环境变量中配置：

AWS_ACCESS_KEY_ID = minioadmin
AWS_SECRET_ACCESS_KEY = minioadmin
AWS_REGION = us-east-1
