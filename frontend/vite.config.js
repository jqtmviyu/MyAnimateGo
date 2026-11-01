import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'
import compression from 'vite-plugin-compression'

let serverHost = process.env['SERVER_HOST'];

let dirname = import.meta.dirname;

export default defineConfig({
    base: './',
    server: {
        port: 37789,
        proxy: {
            '/api': {
                target: serverHost ? serverHost : 'http://127.0.0.1:7789',
                changeOrigin: true,
                secure: false
            }
        }
    },
    plugins: [
        vue(),
        tailwindcss(),
        compression({
            // 输出压缩日志
            verbose: true,
            // 是否禁用压缩
            disable: false,
            // 对超过10KB的文件进行压缩
            threshold: 10240,
            // 使用gzip压缩
            algorithm: 'gzip',
            // 压缩后文件的扩展名
            ext: '.gz'
        }),
    ],
    resolve: {
        alias: {
            '@': path.resolve(dirname, './src/')
        }
    },
    build: {
        chunkSizeWarningLimit: 1024,
        rollupOptions: {
            input: {
                main: path.resolve(dirname, 'index.html')
            },
            output: {
                codeSplitting: {
                    groups: [
                        {
                            name: 'vue',
                            test: /node_modules[\\/](vue|@vueuse[\\/]core|@vicons[\\/]fa)/,
                        },
                        {
                            name: 'utils',
                            test: /node_modules[\\/](js-md5)/,
                        },
                    ]
                },
                chunkFileNames: () => {
                    return `assets/[name]-[hash].js`;
                }
            }
        },
        minify: 'terser',
        terserOptions: {
            compress: {
                drop_console: false,
                drop_debugger: true,
            }
        }
    }
})
