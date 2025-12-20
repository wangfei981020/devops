import yaml
import os
import requests

# 打开并读取 YAML 文件
def read_config(yaml_path='config.yml'):
    with open(yaml_path, 'r', encoding='utf-8') as file:
        data = yaml.safe_load(file)
        # print(data)
    return data

apollo_list = []
# 获取所有 apps
def get_apollo_apps(cookies, apps_url):

    
    """
    使用登录后的会话对象获取 Apollo 系统中的所有应用 (appid)。
    """
    headers = {
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
        "Accept": "application/json",
        "Content-Type": "application/json"
    }
    apoll_apps_url = f'{apps_url}/apps'
    # response = requests.get(url, cookies=cookies)
    response = requests.get(apoll_apps_url, cookies=cookies)
    apollo_appid = []

    if response.status_code == 200:
        apps_data = response.json()
        # print(f"响应内容: {response.text}")  # 打印响应内容
        # print(apps_data)
        for appid in apps_data:
            appid = appid['appId'] 
            apollo_appid.append(appid)
     
    else:
        print(f"获取应用列表失败: {response.status_code}")
    return apollo_appid

def data_json(namespace,key,value):
    data = {
        "namespace": namespace,
        "configurations": [
            {
                "key": key,
                "value": value
            }
        ]
    }
    return data


def get_apollo_value(cookies, apps_url,appid_data,env,cluster,dest_cookie,dest_url):
    apollo_dict = {}
    apollo_value_dict = {}
    apollo_value1_list = []
    for id in appid_data:
        # http://192.168.248.131:8070/apps/gameserver/envs/DEV/clusters/default/namespaces
        apollo_namespaces_url = f'{apps_url}/apps/{id}/envs/{env}/clusters/{cluster}/namespaces'
        # print(apollo_namespaces_url)
        apollo_dict['appid'] = id 

        # print('appid: ',id)
        response = requests.get(apollo_namespaces_url, cookies=cookies)
        # apollo_appid = []

        if response.status_code == 200:
            apps_data = response.json()
            # print(f"响应内容: {response.text}")  # 打印响应内容
            # print(apps_data)
            for n in apps_data:
                namespace = n['baseInfo']['namespaceName']
                apollo_value_dict['namespaceName'] = namespace
                # print('namespace',namespace)
                for k in n['items']:
                    keys = k['item']['key']
                    values = k['item']['value']
                    apollo_value_dict['key'] = keys
                    apollo_value_dict['value'] = values
                    apollo_value1_list.append(apollo_value_dict)
                    apollo_dict['values'] = apollo_value1_list
                    print('-----------')
                    # print('key:',keys,'value: ',values)
                    # print('appid: ',id,'namespace: ',namespace,':',{'key: ',keys,'value: ',values})

                    print(f'{id}: {namespace}: {{{keys}: {values}}}')
                    data_json_result = data_json(namespace,keys,values)
                    print(data_json_result)
                    print('---------------')
                    # dest_url = "http://192.168.248.132:8070/apps/gameserver/envs/DEV/clusters/default/namespaces"
                    
                    http://192.168.248.132:8070/apps/SampleApp/appnamespaces?appendNamespacePrefix=true
                    
                    http://192.168.248.132:8070/apps/SampleApp/envs/DEV/clusters/default/namespaces/redis/item
                    http://192.168.248.132:8070/apps/SampleApp/envs/DEV/clusters/default/namespaces/redis/releases
                    dest_url = f'{dest_url}/apps/{id}/envs/{env}/clusters/{cluster}/namespaces'
                            # 发送 PUT 请求（或者 POST，取决于你的需求）
                    print(dest_url,dest_cookie,data_json_result)
                    response = requests.post(dest_url, cookies=dest_cookie, json=data_json_result)

                    # 检查响应状态
                    if response.status_code == 200:
                        print(f"Namespace {namespace} 配置上传成功！")
                    else:
                        print(f"上传失败: {response.status_code} - {response.text}")

                    apollo_list.append(apollo_dict)
                    apollo_dict = {}
                    apollo_value_dict = {}
                    apollo_value1_list = []

        
        else: 
            print(f"获取namespace列表失败: {response.status_code}")

def main():
    # 获取当前脚本所在的目录
    script_directory = os.path.dirname(os.path.abspath(__file__))
    config_file_path = os.path.join(script_directory, 'config.yml')
    config_file = read_config(config_file_path)
    print(config_file)
    cookie = config_file['apollo']['cookie']
    soure_url = config_file['apollo']['soure_apollo']
    env = config_file['apollo']['env']
    cluster = config_file['apollo']['cluster']
    dest_cookie = config_file['apollo']['dest_cookie']
    dest_url = config_file['apollo']['dest_apollo']
    cookies = {
        'JSESSIONID': cookie  # 登录后的 Session ID
    }
    dest_cookie = {
        'JSESSIONID': dest_cookie  # 登录后的 Session ID
    }
    appid_data = get_apollo_apps(cookies, soure_url)
    get_apollo_value(cookies, soure_url,appid_data,env,cluster,dest_cookie,dest_url)
    # print('apollo_list',apollo_list)



main()


