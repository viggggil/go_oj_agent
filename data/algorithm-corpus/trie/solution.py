n,q=map(int,input().split()); nodes=[{}]; count=[0]
for _ in range(n):
    u=0
    for c in input().strip():
        if c not in nodes[u]: nodes[u][c]=len(nodes); nodes.append({}); count.append(0)
        u=nodes[u][c]; count[u]+=1
for _ in range(q):
    u=0
    for c in input().strip():
        if c not in nodes[u]: u=-1; break
        u=nodes[u][c]
    print(0 if u<0 else count[u])
