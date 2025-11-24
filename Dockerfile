FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS builder

# 这里相当于 cd 到 这个目录下 在容器中 这个目录肯定是没有的  docker会自动创建 这个空目录
WORKDIR /go/src/github.com/tus/tusd


#这一行启动了一个 RUN指令，它会在当前的 Docker 镜像中执行一段 Shell 命令。  set -xe是 Shell 的选项，它的作用是：
#-e：如果任何命令执行失败（返回非零状态码），则立即退出脚本，避免继续执行可能出错的步骤；
#-x：打印执行的每一条命令（调试用，生产环境可考虑去掉，避免泄露细节）； 这里 它说先装这个库 尽可能早 为了使用docker缓存机制
#&& apk add --no-cache gcc libc-dev  这是 Alpine Linux 的包管理工具，类似于 Ubuntu 的 apt或 CentOS 的 yum/dnf。
# Add gcc and libc-dev early so it is cached
RUN set -xe \
	&& apk add --no-cache gcc libc-dev

# 继续使用缓存机制
# Install dependencies earlier so they are cached between builds
COPY go.mod go.sum ./
RUN set -xe \
	&& go mod download

# Copy the source code, because directories are special, there are separate layers
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/

#ARG 参数是 人为从 命令行传递进去的 而不是 从系统环境变量中自动获取的
# Get the version name and git commit as a build argument
ARG GIT_VERSION
ARG GIT_COMMIT

# Get the operating system and architecture to build for
ARG TARGETOS
ARG TARGETARCH

##编译
RUN set -xe \
	&& GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -ldflags="-X github.com/tus/tusd/v2/cmd/tusd/cli.VersionName=${GIT_VERSION} -X github.com/tus/tusd/v2/cmd/tusd/cli.GitCommit=${GIT_COMMIT} -X 'github.com/tus/tusd/v2/cmd/tusd/cli.BuildDate=$(date --utc)'" \
        -o /go/bin/tusd ./cmd/tusd/main.go

# start a new stage that copies in the binary built in the previous stage
FROM alpine:3.22.2
WORKDIR /srv/tusd-data

COPY ./docker/entrypoint.sh /usr/local/share/docker-entrypoint.sh
COPY ./docker/load-env.sh /usr/local/share/load-env.sh

RUN apk add --no-cache ca-certificates jq bash \
    && addgroup -g 1000 tusd \
    && adduser -u 1000 -G tusd -s /bin/sh -D tusd \
    && mkdir -p /srv/tusd-hooks \
    && chown tusd:tusd /srv/tusd-data \
    && chmod +x /usr/local/share/docker-entrypoint.sh /usr/local/share/load-env.sh

COPY --from=builder /go/bin/tusd /usr/local/bin/tusd

EXPOSE 8080
USER tusd
#如果同时定义了 ENTRYPOINT，那么 CMD 的内容会作为参数传给 ENTRYPOINT。
ENTRYPOINT ["/usr/local/share/docker-entrypoint.sh"]
CMD [ "--hooks-dir", "/srv/tusd-hooks" ]
