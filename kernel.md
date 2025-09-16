如何使用busybox和qemu安装linux内核检查内核是否编译成功

首先安装相关包

# Ubuntu/Debian 举例
    sudo apt-get update
    sudo apt-get install -y build-essential bc bison flex libelf-dev libssl-dev dwarves ccache qemu-system-x86 pkg-config gcc g++ libncurses-dev rsync wget xz-utils  
    sudo apt-get install qemu-system 


 
# 这里一定要分开输入和输出文件目录，不然到时候看源码的时候会发现源码和编译后的文件混杂在一起，非常难找。

    export KSRC=/home/xsh/linux-6.2.1
    export KOUT=/home/xsh/build-6.2.1

    mkdir -p "$KOUT"   # 为它们各自准备独立构建输出目录

# 一次性清干净（只清输出树）
    make -C "$KSRC" O="$KOUT" mrproper

# 生成默认配置（x86_64 举例；其他架构可用 ARCH=...）
    make -C "$KSRC" O="$KOUT" x86_64_defconfig
    # 或通用：
    # make -C "$KSRC" O="$KOUT" defconfig

# 开始编译（并把日志保存，别丢到 /dev/null）
    make -C "$KSRC" O="$KOUT" -j"$(nproc)" 2>&1 | tee "$KOUT/build.log"

命令解析：

    make -j$(nproc) : 这是你原本的编译命令。

2>&1: 这个部分是关键。它将标准错误流（2）重定向到标准输出流（1）。在 Linux 中，命令的输出通常分为两种：

标准输出 (stdout)：正常的输出信息。

标准错误 (stderr)：警告和错误信息。
通过 2>&1，我们把所有错误信息也当作普通信息来处理，这样它们就可以一起被导向到下一个命令。

|: 这是一个管道符，它将前一个命令的标准输出（现在包含了正常输出和错误信息）作为下一个命令的标准输入。

tee kernel_build.log: tee 命令接收来自管道的输入，然后将这些信息同时发送到两个地方：

你的终端屏幕。

kernel_build.log 文件。如果该文件不存在，tee 会自动创建它；如果文件已存在，它会覆盖原有内容。


defconfig 之后最好检查一下需要的配置是否被选择。例如 DEBUG 相关的配置、RDMA相关等……

编译好的 bzImage 一般在 arch/x86_64/boot/bzImage.

然后利用 busybox 制作 init ram disk (initrd):

    # 下载 busybox
    wget https://busybox.net/downloads/busybox-1.36.1.tar.bz2
    tar xvf busybox-1.36.1.tar.bz2
    cd busybox-1.36.1/

    make help # 查看如何构建

    # 基本上按照以下步骤
    make defconfig
    make menuconfig 
    # 在 menuconfig 中 Settings --> [*] Build static binary (no shared libs)
    # 在 menuconfig 中→ Networking Utilities → 取消勾选 tc  ，tc工具可能在高版本内核中有点问题。
    # 选择静态二进制，不依赖于动态库

    make -j32 > /dev/null
    make install

make install 执行完，二进制文件会被安装在 _install 目录下：

    cd _install

    mkdir proc
    mkdir sys
    mkdir dev

    vim init
在 init 文件中写入以下内容：

    #!/bin/sh

    mount -t devtmpfs devtmpfs /dev
    mount -t proc proc /proc
    mount -t sysfs sysfs /sys
    echo "Welcome to my Linux!"
    exec /bin/sh
然后为 init 脚本加入执行权限

    sudo chmod +x init
然后将 _install 目录下所有内容打包为 initrd:

    find . -print0 | cpio --null -ov --format=newc | gzip -9 > ../initramfs.cpio.gz
创建 run.sh 文件用于调用 qemu 启动内核：

# contents of run.sh
    #!/bin/sh
    qemu-system-x86_64 \
    -kernel /mnt/sda/qemu/kernel/6.1.55-defconfig/arch/x86_64/boot/bzImage \
    -initrd  /mnt/sda/qemu/busybox-1.36.1/initramfs.cpio.gz \
    -append "root=/dev/ram rw console=ttyS0" \
    -nographic \
    -netdev user,id=t0, -device e1000,netdev=t0,id=nic0 \
    -m 64M \
    -monitor /dev/null \
    -smp cores=2,threads=1 \
    -cpu kvm64,+smep \
    #-S 启动gdb调试 \
    #-gdb tcp:1234 等待gdb调试
启动后进入到 busybox 的主目录(initrd 目录)。如果不能正确启动，大概率是内核编译的时候配置不完整。

需要注意的是，busybox 目录下没有 /lib/modules，在 init 脚本下无法加载模块。如果需要，可以将编译好的模块放到 busybox 目录下，在 init 脚本中 insmod. （简单的方法 - 直接在配置内核时将需要的模块选为 built-in）