import {updateRuntimeState} from '@/lib/runtime-state'
import {notify} from "@/lib/feedback";
import {authorization} from "@/js/global.js";

let post = async (url, body, options = {}) => {
    return await fetch_(url, 'POST', body, options);
}

let get = async (url, options = {}) => {
    return await fetch_(url, 'GET', '', options);
}

let del = async (url, body) => {
    return await fetch_(url, 'DELETE', body);
}

let put = async (url, body) => {
    return await fetch_(url, 'PUT', body);
}

let fetch_ = async (url, method, body, options = {}) => {
    let headers = {}
    if (authorization.value) {
        headers['Authorization'] = authorization.value
    }
    if (body) {
        headers['Content-Type'] = 'application/json'
    }
    return await fetch(url, {
        method: method,
        body: body ? JSON.stringify(body) : null,
        headers: headers,
        signal: options.signal
    })
        .then(res => res.json())
        .catch((cause) => {
            if (cause.name === 'AbortError') throw cause
            const error = new Error('请求未完成，请检查服务连接后重试')
            if (!options.silent) notify.error(error.message)
            throw error
        })
        .then(res => {
            let {code, message, t} = res

            if (!checkTimestampRange(t, true)) {
                console.warn('与服务端时差超过30分钟')
            }

            if (code >= 200 && code < 300) {
                if (['api/config', 'api/setautomatictaskspaused'].includes(url.split('?')[0].toLowerCase())) updateRuntimeState(res.data)
                return res
            }

            if (!options.silent) {
                notify.error(message)
            }
            if (code === 403) {
                authorization.value = ''
                setTimeout(() => {
                    location.reload()
                }, 1000)
            }
            return new Promise((resolve, reject) => {
                reject(new Error(message));
            });
        })
}

export default {post, get, del, put}

let checkTimestampRange = (timestamp, isMilli = true) => {
    const ts = Math.floor(Number(timestamp));
    if (Number.isNaN(ts)) return false;
    const targetTime = isMilli ? ts : ts * 1000;
    const now = Date.now();
    // 30 分钟
    const range = 30 * 60 * 1000;
    const diff = Math.abs(now - targetTime);
    return diff <= range;
}
