import AppKit
import CoreGraphics
import Foundation

let output = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "frontend/public", isDirectory: true)
func png(_ size: Int) -> Data {
    let context = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: size * 4,
                            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
    context.translateBy(x: 0, y: CGFloat(size))
    context.scaleBy(x: CGFloat(size) / 24, y: -CGFloat(size) / 24)
    context.setStrokeColor(CGColor(red: 82 / 255, green: 181 / 255, blue: 75 / 255, alpha: 1))
    context.setLineWidth(2)
    context.setLineCap(.round)
    context.setLineJoin(.round)
    context.move(to: CGPoint(x: 4, y: 11))
    context.addCurve(to: CGPoint(x: 13, y: 20), control1: CGPoint(x: 8.97056, y: 11), control2: CGPoint(x: 13, y: 15.02944))
    context.strokePath()
    context.move(to: CGPoint(x: 4, y: 4))
    context.addCurve(to: CGPoint(x: 20, y: 20), control1: CGPoint(x: 12.83656, y: 4), control2: CGPoint(x: 20, y: 11.16344))
    context.strokePath()
    context.strokeEllipse(in: CGRect(x: 4, y: 18, width: 2, height: 2))
    return NSBitmapImageRep(cgImage: context.makeImage()!).representation(using: .png, properties: [:])!
}
for size in [128, 256, 512] {
    try png(size).write(to: output.appendingPathComponent("icon-\(size).png"))
}
// ICO 内嵌不同尺寸的 PNG，兼容使用传统 favicon 的浏览器。
let sizes = [16, 32, 48, 64, 128, 256]
let images = sizes.map(png)
var icon = Data()
func write16(_ n: UInt16) { var value = n.littleEndian; withUnsafeBytes(of: &value) { icon.append(contentsOf: $0) } }
func write32(_ n: UInt32) { var value = n.littleEndian; withUnsafeBytes(of: &value) { icon.append(contentsOf: $0) } }
write16(0); write16(1); write16(UInt16(sizes.count))
var offset = 6 + sizes.count * 16
for (size, data) in zip(sizes, images) {
    icon.append(contentsOf: [UInt8(size % 256), UInt8(size % 256), 0, 0])
    write16(1); write16(32); write32(UInt32(data.count)); write32(UInt32(offset))
    offset += data.count
}
for data in images { icon.append(data) }
try icon.write(to: output.appendingPathComponent("favicon.ico"))
print("绿色 RSS PNG 与 ICO 已生成")
