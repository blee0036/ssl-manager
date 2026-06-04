/** 域名正则 */
export const DOMAIN_REG = /^(\*\.)?([a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/;

/** IP 地址正则 */
export const IP_REG = /^(\d{1,3}\.){3}\d{1,3}$/;

/** 端口正则 */
export const PORT_REG = /^([1-9]\d{0,4}|[1-5]\d{4}|6[0-4]\d{3}|65[0-4]\d{2}|655[0-2]\d|6553[0-5])$/;
