#!/usr/bin/env python3
"""把 Safari「添加到程序坞」生成的 web app 图标替换为我们自己的方章 icns。

背景（2026-10-01 实测）：Safari 挑 manifest 图标时无条件抢 maskable，且生成 icns 时
把内容缩进 80% 模板、字再压一档 —— Dock 里字小。本脚本绕开整条链路：

  web/public/icons/icon-dock-1024.png（满幅方章，gen-icons.mjs 渲染）
    → 按 Apple 模板比例（1024 画布、内容 824 居中）合成
    → iconset 各尺寸 → iconutil 打包 icns
    → 备份原文件后替换 ~/Applications/<app>.app/Contents/Resources/ApplicationIcon.icns
    → ad-hoc 重签名（替换资源后原签名失效）

用法：
  python3 scripts/make-dock-icon.py 慎始          # app 名（~/Applications/<名>.app）
  python3 scripts/make-dock-icon.py 慎始 --no-restart   # 不重启 Dock

重新「添加到程序坞」后 icns 会被 Safari 重新生成，届时重跑本脚本即可。
"""
import argparse
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HOME = Path.home()
CANVAS = 1024
CONTENT = 824          # Apple 图标模板：内容占 1024 的 80%
SIZES = [16, 32, 64, 128, 256, 512, 1024]   # iconset：16/32/128/256/512 各含 @2x


def run(*cmd):
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"命令失败：{' '.join(cmd)}\n{r.stderr.strip()}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('app', help='web app 名称（~/Applications/<名>.app）')
    ap.add_argument('--no-restart', action='store_true', help='不重启 Dock')
    ap.add_argument('--src', default=HOME and '/Users/yufei/Git/shenshi/web/public/icons/icon-dock-1024.png')
    args = ap.parse_args()

    app_dir = HOME / 'Applications' / f'{args.app}.app'
    icns = app_dir / 'Contents' / 'Resources' / 'ApplicationIcon.icns'
    if not icns.exists():
        sys.exit(f'找不到 {icns}，确认 web app 名称。')
    src = Path(args.src)
    if not src.exists():
        sys.exit(f'找不到源图 {src}，先跑 cd web && pnpm gen:icons。')

    from PIL import Image

    img = Image.open(src).convert('RGBA').resize((CONTENT, CONTENT), Image.LANCZOS)
    canvas = Image.new('RGBA', (CANVAS, CANVAS), (0, 0, 0, 0))
    canvas.paste(img, ((CANVAS - CONTENT) // 2, (CANVAS - CONTENT) // 2), img)

    with tempfile.TemporaryDirectory() as td:
        iconset = Path(td) / 'ApplicationIcon.iconset'
        iconset.mkdir()
        for s in SIZES:
            for name, px in ((f'icon_{s}x{s}.png', s), (f'icon_{s}x{s}@2x.png', s * 2)):
                if px > CANVAS:
                    continue
                canvas.resize((px, px), Image.LANCZOS).save(iconset / name)
        run('iconutil', '-c', 'icns', str(iconset), '-o', str(Path(td) / 'ApplicationIcon.icns'))

        backup = icns.with_suffix('.icns.bak')
        if not backup.exists():
            shutil.copy2(icns, backup)
            print(f'  已备份原图标 → {backup}')
        shutil.copy2(Path(td) / 'ApplicationIcon.icns', icns)
        print(f'  已替换 {icns}')

    # 替换资源后原签名失效，ad-hoc 重签，否则应用可能拒绝启动。
    run('codesign', '--force', '--deep', '--sign', '-', str(app_dir))
    print('  已 ad-hoc 重签名')

    if not args.no_restart:
        subprocess.run(['killall', 'Dock'], capture_output=True)
        print('  已重启 Dock（桌面会闪一下）')

    print(f'完成：{app_dir}')


if __name__ == '__main__':
    main()
