import os

# 读取基础模板
with open('style-01-glass.html', 'r', encoding='utf-8') as f:
    base_template = f.read()

# 20种风格配置
styles = [
    # (编号, 文件名, 标题, CSS样式)
    (2, 'style-02-dark.html', '深黑模式', '''
        :root {
            --primary: #3b82f6;
            --secondary: #8b5cf6;
            --success: #22c55e;
            --warning: #f59e0b;
            --error: #ef4444;
            --glass-bg: #111827;
            --glass-border: #1f2937;
            --text: #f8fafc;
            --text-secondary: #94a3b8;
        }
        body { background: #000; }
        .stat-card:nth-child(1) .stat-value { color: #3b82f6; }
        .stat-card:nth-child(2) .stat-value { color: #22c55e; }
        .stat-card:nth-child(3) .stat-value { color: #f59e0b; }
        .stat-card:nth-child(4) .stat-value { color: #8b5cf6; }
        .chart-bar { background: linear-gradient(180deg, #3b82f6, #1d4ed8); }
    '''),
    (3, 'style-03-neumorphism.html', '新拟态', '''
        :root {
            --primary: #3b82f6;
            --glass-bg: #e0e5ec;
            --glass-border: transparent;
            --text: #333;
            --text-secondary: #666;
        }
        body { background: #e0e5ec; color: #333; }
        .sidebar { box-shadow: inset -5px 0 10px rgba(0,0,0,0.05); }
        .logo-icon, .stat-card, .card, .search-box, .icon-btn {
            background: #e0e5ec;
            box-shadow: 5px 5px 15px #b8bec5, -5px -5px 15px #fff;
            border: none;
        }
        .nav-header:hover, .nav-header.active, .nav-item:hover, .nav-item.active {
            box-shadow: inset 3px 3px 6px #b8bec5, inset -3px -3px 6px #fff;
            background: #e0e5ec;
        }
        .chart { box-shadow: inset 5px 5px 10px #b8bec5, inset -5px -5px 10px #fff; background: #e0e5ec; }
        .chart-bar { background: linear-gradient(180deg, #3b82f6, #60a5fa); border-radius: 20px; }
        .stat-value { color: #3b82f6; }
    '''),
    (4, 'style-04-cyberpunk.html', '赛博朋克', '''
        :root {
            --primary: #ff0080;
            --secondary: #00ffff;
            --glass-bg: rgba(255,0,128,0.1);
            --glass-border: #ff0080;
            --text: #fff;
            --text-secondary: #00ffff;
        }
        body { background: linear-gradient(180deg, #0a0014, #1a0028); }
        .sidebar { border-right: 2px solid #ff0080; }
        .logo-icon { background: #ff0080; box-shadow: 0 0 30px #ff0080; }
        .stat-card, .card { border: 1px solid #00ffff; background: rgba(0,255,255,0.1); }
        .stat-value { color: #00ffff; text-shadow: 0 0 10px #00ffff; }
        .chart-bar { background: linear-gradient(180deg, #ff0080, #00ffff); box-shadow: 0 0 15px #ff0080; }
        .nav-item.active { background: rgba(0,255,255,0.2); border-left: 2px solid #00ffff; }
    '''),
    (5, 'style-05-minimal.html', '极简主义', '''
        :root {
            --primary: #111;
            --glass-bg: #fff;
            --glass-border: #eee;
            --text: #111;
            --text-secondary: #666;
        }
        body { background: #fff; color: #111; }
        .sidebar { background: #fafafa; border-right: 1px solid #eee; }
        .logo-icon { background: #111; color: #fff; }
        .stat-card, .card { background: #fafafa; border: none; }
        .chart { background: #f5f5f5; }
        .chart-bar { background: #111; }
        .service { background: #f5f5f5; }
    '''),
    (6, 'style-06-aurora.html', '极光', '''
        :root {
            --primary: #00ff88;
            --secondary: #00c8ff;
            --glass-bg: rgba(255,255,255,0.05);
            --glass-border: rgba(255,255,255,0.1);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.7);
        }
        body { background: linear-gradient(135deg, #0f0c29, #302b63, #24243e); }
        body::before {
            content: '';
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            height: 300px;
            background: linear-gradient(180deg, rgba(0,255,136,0.3), rgba(0,200,255,0.2), transparent);
            filter: blur(60px);
            pointer-events: none;
        }
        .logo-icon { background: linear-gradient(135deg, #00ff88, #00c8ff); }
        .stat-value { background: linear-gradient(135deg, #00ff88, #00c8ff); -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
        .chart-bar { background: linear-gradient(180deg, #00ff88, #00c8ff); }
    '''),
    (7, 'style-07-corporate.html', '企业蓝', '''
        :root {
            --primary: #1e3a5f;
            --secondary: #3b82f6;
            --glass-bg: #fff;
            --glass-border: #e5e7eb;
            --text: #1e3a5f;
            --text-secondary: #64748b;
        }
        body { background: #f0f4f8; color: #1e3a5f; }
        .sidebar { background: #1e3a5f; }
        .sidebar .nav-header, .sidebar .nav-item, .sidebar .logo-text, .sidebar-footer { color: rgba(255,255,255,0.8); }
        .sidebar .nav-header:hover, .sidebar .nav-item:hover { background: rgba(255,255,255,0.1); color: #fff; }
        .sidebar .nav-header.active, .sidebar .nav-item.active { background: #3b82f6; color: #fff; }
        .logo-icon { background: #fff; color: #1e3a5f; }
        .stat-card, .card { background: #fff; box-shadow: 0 2px 8px rgba(0,0,0,0.08); border: none; }
        .chart-bar { background: #3b82f6; }
    '''),
    (8, 'style-08-terminal.html', '终端风格', '''
        :root {
            --primary: #22c55e;
            --glass-bg: #0d1117;
            --glass-border: #21262d;
            --text: #22c55e;
            --text-secondary: rgba(34,197,94,0.7);
        }
        body { background: #010409; font-family: 'JetBrains Mono', monospace; }
        .sidebar { background: #0d1117; border-right: 1px solid #21262d; }
        .logo-icon { background: transparent; border: 1px solid #22c55e; }
        .stat-card, .card { background: #0d1117; border: 1px solid #21262d; }
        .stat-value, .card-title, .page-title { color: #22c55e; }
        .chart { background: #010409; border: 1px solid #21262d; }
        .chart-bar { background: #22c55e; }
        .service { background: #0d1117; border: 1px solid #21262d; }
    '''),
    (9, 'style-09-gradient.html', '渐变丰富', '''
        :root {
            --primary: #ff6b6b;
            --glass-bg: rgba(0,0,0,0.3);
            --glass-border: rgba(255,255,255,0.2);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.8);
        }
        body { background: linear-gradient(135deg, #ff6b6b, #feca57, #48dbfb, #ff9ff3, #54a0ff); background-size: 400% 400%; animation: gradientMove 15s ease infinite; }
        @keyframes gradientMove { 0%, 100% { background-position: 0% 50%; } 50% { background-position: 100% 50%; } }
        .sidebar { backdrop-filter: blur(20px); }
        .logo-icon { background: #fff; color: #ff6b6b; }
        .stat-card, .card { background: rgba(255,255,255,0.2); backdrop-filter: blur(20px); }
        .chart-bar { background: #fff; }
    '''),
    (10, 'style-10-neon.html', '霓虹发光', '''
        :root {
            --primary: #8b5cf6;
            --glass-bg: #111;
            --glass-border: #333;
            --text: #fff;
            --text-secondary: #94a3b8;
        }
        body { background: #0a0a0a; }
        .logo-icon { background: #8b5cf6; box-shadow: 0 0 40px #8b5cf6; }
        .stat-card:nth-child(1) { border: 1px solid #8b5cf6; box-shadow: 0 0 20px rgba(139,92,246,0.3); }
        .stat-card:nth-child(1) .stat-value { color: #8b5cf6; text-shadow: 0 0 20px #8b5cf6; }
        .stat-card:nth-child(2) .stat-value { color: #22c55e; text-shadow: 0 0 20px #22c55e; }
        .stat-card:nth-child(3) .stat-value { color: #f59e0b; text-shadow: 0 0 20px #f59e0b; }
        .stat-card:nth-child(4) .stat-value { color: #06b6d4; text-shadow: 0 0 20px #06b6d4; }
        .chart-bar { background: linear-gradient(180deg, #8b5cf6, #3b82f6); box-shadow: 0 0 15px #8b5cf6; }
    '''),
    (11, 'style-11-ocean.html', '海洋波浪', '''
        :root {
            --primary: #90e0ef;
            --glass-bg: rgba(0,0,0,0.3);
            --glass-border: rgba(144,224,239,0.3);
            --text: #fff;
            --text-secondary: #90e0ef;
        }
        body { background: linear-gradient(180deg, #0077b6, #023e8a, #03045e); }
        .logo-icon { background: #90e0ef; color: #03045e; }
        .stat-value { color: #90e0ef; }
        .chart-bar { background: linear-gradient(180deg, #90e0ef, #48cae4); }
        .nav-item.active { background: rgba(144,224,239,0.2); }
    '''),
    (12, 'style-12-sunset.html', '日落暖色', '''
        :root {
            --primary: #ff512f;
            --glass-bg: rgba(0,0,0,0.2);
            --glass-border: rgba(255,255,255,0.2);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.8);
        }
        body { background: linear-gradient(135deg, #ff512f, #f09819); }
        .logo-icon { background: #fff; color: #ff512f; }
        .stat-card, .card { background: rgba(255,255,255,0.15); }
        .chart-bar { background: #fff; }
    '''),
    (13, 'style-13-forest.html', '森林自然', '''
        :root {
            --primary: #34d399;
            --glass-bg: rgba(0,0,0,0.3);
            --glass-border: rgba(52,211,153,0.3);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.7);
        }
        body { background: linear-gradient(180deg, #134e4a, #0f3d3a, #022c22); }
        .logo-icon { background: #34d399; color: #022c22; }
        .stat-value { color: #34d399; }
        .chart-bar { background: linear-gradient(180deg, #34d399, #059669); }
    '''),
    (14, 'style-14-mono.html', '单色', '''
        :root {
            --primary: #fff;
            --glass-bg: #27272a;
            --glass-border: #3f3f46;
            --text: #fff;
            --text-secondary: #a1a1aa;
        }
        body { background: #18181b; }
        .sidebar { background: #09090b; }
        .logo-icon { background: #fff; color: #000; }
        .chart-bar { background: linear-gradient(180deg, #fff, #a1a1aa); }
    '''),
    (15, 'style-15-vibrant.html', '鲜艳大胆', '''
        :root {
            --primary: #e94560;
            --glass-bg: #16213e;
            --glass-border: #0f3460;
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.7);
        }
        body { background: #1a1a2e; }
        .sidebar { border-right: 2px solid #e94560; }
        .logo-icon { background: #e94560; }
        .stat-card:nth-child(1) .stat-value { color: #e94560; }
        .stat-card:nth-child(2) .stat-value { color: #00d9ff; }
        .stat-card:nth-child(3) .stat-value { color: #00ff88; }
        .stat-card:nth-child(4) .stat-value { color: #ffd700; }
        .chart-bar { background: linear-gradient(180deg, #e94560, #00d9ff); }
    '''),
    (16, 'style-16-elegant.html', '优雅深色', '''
        :root {
            --primary: #c9a227;
            --glass-bg: rgba(255,255,255,0.03);
            --glass-border: rgba(255,255,255,0.08);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.6);
        }
        body { background: linear-gradient(180deg, #1a1a2e, #0f0f1a); }
        .logo-icon { background: linear-gradient(135deg, #c9a227, #f4d03f); color: #1a1a2e; }
        .stat-value { color: #c9a227; }
        .chart-bar { background: linear-gradient(180deg, #c9a227, #f4d03f); }
        .nav-item.active { border-left: 2px solid #c9a227; }
    '''),
    (17, 'style-17-material.html', '材质设计', '''
        :root {
            --primary: #6200ea;
            --glass-bg: #fff;
            --glass-border: #e0e0e0;
            --text: #212121;
            --text-secondary: #757575;
        }
        body { background: #fafafa; color: #212121; }
        .sidebar { background: #6200ea; }
        .sidebar .nav-header, .sidebar .nav-item, .sidebar .logo-text, .sidebar-footer { color: rgba(255,255,255,0.9); }
        .sidebar .nav-header:hover, .sidebar .nav-item:hover { background: rgba(255,255,255,0.1); }
        .sidebar .nav-item.active { background: rgba(255,255,255,0.2); }
        .logo-icon { background: #fff; color: #6200ea; }
        .stat-card, .card { box-shadow: 0 2px 8px rgba(0,0,0,0.1); border: none; }
        .stat-value { color: #6200ea; }
        .chart-bar { background: #6200ea; }
    '''),
    (18, 'style-18-clay.html', '粘土态', '''
        :root {
            --primary: #4285f4;
            --glass-bg: #e8f0fe;
            --glass-border: transparent;
            --text: #333;
            --text-secondary: #666;
        }
        body { background: #e8f0fe; color: #333; }
        .sidebar { background: #d4e4fc; border-radius: 0 24px 24px 0; box-shadow: 8px 8px 20px rgba(0,0,0,0.1), -8px -8px 20px #fff; }
        .logo-icon { background: #4285f4; border-radius: 14px; box-shadow: 4px 4px 10px rgba(0,0,0,0.15); }
        .stat-card, .card { background: #e8f0fe; border-radius: 20px; box-shadow: 8px 8px 20px rgba(0,0,0,0.1), -8px -8px 20px #fff; border: none; }
        .chart { box-shadow: inset 4px 4px 10px rgba(0,0,0,0.08), inset -4px -4px 10px #fff; }
        .chart-bar { background: #4285f4; border-radius: 10px; }
        .stat-value { color: #4285f4; }
    '''),
    (19, 'style-19-brutalism.html', '野蛮主义', '''
        :root {
            --primary: #000;
            --glass-bg: #fff;
            --glass-border: #000;
            --text: #000;
            --text-secondary: #333;
        }
        body { background: #fff; color: #000; }
        .sidebar { background: #000; color: #fff; }
        .sidebar .nav-header, .sidebar .nav-item, .sidebar .logo-text, .sidebar-footer { color: #fff; }
        .sidebar .nav-item.active { background: #ffff00; color: #000; }
        .logo-icon { background: #ff0000; border: 3px solid #000; }
        .stat-card, .card { border: 3px solid #000; border-radius: 0; }
        .stat-card:nth-child(1) { background: #ff0000; color: #fff; }
        .stat-card:nth-child(2) { background: #0000ff; color: #fff; }
        .stat-card:nth-child(3) { background: #ffff00; }
        .stat-card:nth-child(4) { background: #00ff00; }
        .chart { border: 3px solid #000; border-radius: 0; }
        .chart-bar { background: #000; border-radius: 0; }
    '''),
    (20, 'style-20-flat.html', '扁平设计', '''
        :root {
            --primary: #3498db;
            --glass-bg: #fff;
            --glass-border: #ecf0f1;
            --text: #2c3e50;
            --text-secondary: #7f8c8d;
        }
        body { background: #ecf0f1; color: #2c3e50; }
        .sidebar { background: #2c3e50; }
        .sidebar .nav-header, .sidebar .nav-item, .sidebar .logo-text, .sidebar-footer { color: rgba(255,255,255,0.9); }
        .sidebar .nav-item.active { background: #3498db; }
        .logo-icon { background: #3498db; }
        .stat-card, .card { border: none; }
        .stat-card:nth-child(1) .stat-value { color: #3498db; }
        .stat-card:nth-child(2) .stat-value { color: #2ecc71; }
        .stat-card:nth-child(3) .stat-value { color: #e74c3c; }
        .stat-card:nth-child(4) .stat-value { color: #9b59b6; }
        .chart-bar { background: #3498db; }
    '''),
]

# 生成每个风格的页面
for num, filename, title, css in styles:
    # 替换CSS
    new_content = base_template.replace(
        '''        :root {
            --primary: #667eea;
            --secondary: #764ba2;
            --accent: #f093fb;
            --success: #22c55e;
            --warning: #f59e0b;
            --error: #ef4444;
            --glass-bg: rgba(255,255,255,0.1);
            --glass-border: rgba(255,255,255,0.2);
            --text: #fff;
            --text-secondary: rgba(255,255,255,0.7);
        }

        body {
            font-family: 'Inter', sans-serif;
            min-height: 100vh;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 50%, #f093fb 100%);
            color: var(--text);
            overflow-x: hidden;
        }''',
        css + '''
        body {
            font-family: 'Inter', sans-serif;
            min-height: 100vh;
            color: var(--text);
            overflow-x: hidden;
        }'''
    )
    
    # 替换标题
    new_content = new_content.replace('风格01 玻璃态', f'风格{num:02d} {title}')
    new_content = new_content.replace('风格 01: Glassmorphism 玻璃态', f'风格 {num:02d}: {title}')
    
    # 更新下拉选择器的选中项
    new_content = new_content.replace(
        f'<option value="{filename}">',
        f'<option value="{filename}" selected>'
    )
    new_content = new_content.replace(
        '<option value="style-01-glass.html" selected>',
        '<option value="style-01-glass.html">'
    )
    
    # 写入文件
    with open(filename, 'w', encoding='utf-8') as f:
        f.write(new_content)
    
    print(f'Generated: {filename}')

print('\\nAll 20 styles generated!')
