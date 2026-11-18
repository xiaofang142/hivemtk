// 直接导入的JSON由构建插件预编译为消息函数，不引入运行时编译。
import ar from './locales/ar.json'
import de from './locales/de.json'
import en from './locales/en.json'
import es from './locales/es.json'
import fr from './locales/fr.json'
import ja from './locales/ja.json'
import pt from './locales/pt.json'
import ru from './locales/ru.json'
import zh from './locales/zh.json'

export const messages = { ar, de, en, es, fr, ja, pt, ru, zh }
