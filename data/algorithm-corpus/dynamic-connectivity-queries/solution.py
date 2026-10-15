n,q=map(int,input().split()); p=list(range(n)); size=[1]*n
def find(x):
    while p[x]!=x: p[x]=p[p[x]]; x=p[x]
    return x
for _ in range(q):
    op,u,v=input().split(); a,b=find(int(u)-1),find(int(v)-1)
    if op=='Q': print('YES' if a==b else 'NO')
    elif a!=b:
        if size[a]<size[b]: a,b=b,a
        p[b]=a; size[a]+=size[b]
