-- Enterprise Wiki v6.0.0 Production Schema
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255),
    auth_provider VARCHAR(50) DEFAULT 'auth.local',
    role VARCHAR(50) DEFAULT 'Viewer',
    group_name VARCHAR(100) DEFAULT 'General',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS categories (
    id SERIAL PRIMARY KEY,
    path VARCHAR(500) UNIQUE NOT NULL,
    title JSONB NOT NULL,
    tags TEXT[] DEFAULT '{}',
    is_public_guest BOOLEAN DEFAULT FALSE,
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS elements (
    id SERIAL PRIMARY KEY,
    category_path VARCHAR(500) NOT NULL REFERENCES categories(path) ON DELETE CASCADE,
    plugin_type VARCHAR(100) NOT NULL,
    title JSONB NOT NULL,
    content JSONB NOT NULL,
    tags TEXT[] DEFAULT '{}',
    version INT NOT NULL DEFAULT 1,
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS acl_rules (
    id SERIAL PRIMARY KEY,
    resource_type VARCHAR(50) NOT NULL,
    resource_id VARCHAR(500) NOT NULL,
    principal_type VARCHAR(20) NOT NULL,
    principal_id VARCHAR(100) NOT NULL,
    permission VARCHAR(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS menus (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    title JSONB NOT NULL,
    location VARCHAR(50) NOT NULL,
    items JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
